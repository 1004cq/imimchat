package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	mlsDeviceJoinPrefix = "mls:device-join:"
	mlsDeviceJoinTTL    = 10 * time.Minute
	mlsDeviceClaimTTL   = time.Minute
)

type mlsDeviceJoinRecord struct {
	RequestID         string          `json:"requestId"`
	GroupID           string          `json:"groupId"`
	UserID            string          `json:"userId"`
	DeviceID          string          `json:"deviceId"`
	MemberID          string          `json:"memberId"`
	KeyPackage        json.RawMessage `json:"keyPackage"`
	Status            string          `json:"status"`
	CreatedAt         time.Time       `json:"createdAt"`
	ExpiresAt         time.Time       `json:"expiresAt"`
	ClaimedBy         string          `json:"claimedBy,omitempty"`
	ClaimUntil        *time.Time      `json:"claimUntil,omitempty"`
	Welcome           json.RawMessage `json:"welcome,omitempty"`
	SenderIdentityKey string          `json:"senderIdentityKey,omitempty"`
}

type mlsKeyPackageHeader struct {
	InitKey string `json:"initKey"`
	LeafKey string `json:"leafKey"`
	UserID  string `json:"userId"`
}

type mlsWelcomeHeader struct {
	GroupID            string          `json:"groupId"`
	Epoch              int             `json:"epoch"`
	LeafIndex          int             `json:"leafIndex"`
	Members            map[string]int  `json:"members"`
	TreeSnapshot       json.RawMessage `json:"treeSnapshot"`
	EncryptedGroupInfo struct {
		Ciphertext string `json:"ciphertext"`
		IV         string `json:"iv"`
	} `json:"encryptedGroupInfo"`
}

func (s *Server) registerMLSDeviceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/mls/device-join/request", s.requireUser(s.mlsDeviceJoinRequest))
	mux.HandleFunc("GET /api/mls/device-join/pending", s.requireUser(s.mlsDeviceJoinPending))
	mux.HandleFunc("POST /api/mls/device-join/claim", s.requireUser(s.mlsDeviceJoinClaim))
	mux.HandleFunc("POST /api/mls/device-join/complete", s.requireUser(s.mlsDeviceJoinComplete))
	mux.HandleFunc("GET /api/mls/device-join/welcome", s.requireUser(s.mlsDeviceJoinWelcome))
	mux.HandleFunc("POST /api/mls/device-join/ack", s.requireUser(s.mlsDeviceJoinAck))
}

func mlsDeviceRequestID(groupID, userID, deviceID string) string {
	sum := sha256.Sum256([]byte(groupID + "\x00" + userID + "\x00" + deviceID))
	return hex.EncodeToString(sum[:])
}

func mlsDeviceKey(requestID string) string { return mlsDeviceJoinPrefix + requestID }

func (s *Server) isMLSGroupMember(r *http.Request, groupID, userID string) bool {
	var ok bool
	_ = s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM "GroupMember" WHERE "groupId"=$1 AND "userId"=$2)`, groupID, userID).Scan(&ok)
	return ok
}

func (s *Server) readMLSDeviceJoin(r *http.Request, requestID string) (mlsDeviceJoinRecord, string, error) {
	var raw string
	err := s.db.QueryRow(r.Context(), `SELECT "value" FROM "SystemConfig" WHERE "key"=$1`, mlsDeviceKey(requestID)).Scan(&raw)
	if err != nil {
		return mlsDeviceJoinRecord{}, "", err
	}
	var record mlsDeviceJoinRecord
	if err = json.Unmarshal([]byte(raw), &record); err != nil {
		return mlsDeviceJoinRecord{}, raw, err
	}
	return record, raw, nil
}

func (s *Server) writeMLSDeviceJoin(r *http.Request, record mlsDeviceJoinRecord) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return s.upsertSystemConfig(r.Context(), mlsDeviceKey(record.RequestID), string(raw))
}

func (s *Server) mlsDeviceJoinRequest(w http.ResponseWriter, r *http.Request, u user) {
	var in struct {
		GroupID    string          `json:"groupId"`
		DeviceID   string          `json:"deviceId"`
		KeyPackage json.RawMessage `json:"keyPackage"`
	}
	if decode(r, &in) != nil || strings.TrimSpace(in.GroupID) == "" || strings.TrimSpace(in.DeviceID) == "" || len(in.KeyPackage) == 0 {
		groupBad(w, "缺少设备加入参数")
		return
	}
	var keyPackage mlsKeyPackageHeader
	if json.Unmarshal(in.KeyPackage, &keyPackage) != nil || keyPackage.InitKey == "" || keyPackage.LeafKey == "" || keyPackage.UserID != u.ID {
		groupBad(w, "KeyPackage 用户或公钥无效")
		return
	}
	if !s.isMLSGroupMember(r, in.GroupID, u.ID) {
		writeJSON(w, 403, map[string]string{"error": "非群成员"})
		return
	}
	deviceID := strings.TrimSpace(in.DeviceID)
	if len(deviceID) > 128 {
		deviceID = deviceID[:128]
	}
	requestID := mlsDeviceRequestID(in.GroupID, u.ID, deviceID)
	if record, _, err := s.readMLSDeviceJoin(r, requestID); err == nil && record.ExpiresAt.After(time.Now()) {
		claimActive := record.Status == "claimed" && record.ClaimUntil != nil && record.ClaimUntil.After(time.Now())
		if record.Status == "completed" || claimActive {
			writeJSON(w, 200, map[string]any{"ok": true, "requestId": requestID, "status": record.Status})
			return
		}
	}
	now := time.Now().UTC()
	record := mlsDeviceJoinRecord{RequestID: requestID, GroupID: in.GroupID, UserID: u.ID, DeviceID: deviceID, MemberID: u.ID + "#ios#" + deviceID, KeyPackage: in.KeyPackage, Status: "pending", CreatedAt: now, ExpiresAt: now.Add(mlsDeviceJoinTTL)}
	if err := s.writeMLSDeviceJoin(r, record); err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "requestId": requestID, "status": "pending"})
}

func (s *Server) mlsDeviceJoinPending(w http.ResponseWriter, r *http.Request, u user) {
	groupID := strings.TrimSpace(r.URL.Query().Get("groupId"))
	if groupID == "" {
		groupBad(w, "缺少 groupId")
		return
	}
	if !s.isMLSGroupMember(r, groupID, u.ID) {
		writeJSON(w, 403, map[string]string{"error": "非群成员"})
		return
	}
	rows, err := s.db.Query(r.Context(), `SELECT "value" FROM "SystemConfig" WHERE "key" LIKE $1`, mlsDeviceJoinPrefix+"%")
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	now := time.Now()
	requests := []mlsDeviceJoinRecord{}
	for rows.Next() {
		var raw string
		if rows.Scan(&raw) != nil {
			continue
		}
		var record mlsDeviceJoinRecord
		if json.Unmarshal([]byte(raw), &record) != nil || record.GroupID != groupID || !record.ExpiresAt.After(now) {
			continue
		}
		claimExpired := record.Status == "claimed" && (record.ClaimUntil == nil || !record.ClaimUntil.After(now))
		if record.Status == "pending" || claimExpired {
			requests = append(requests, record)
		}
	}
	writeJSON(w, 200, map[string]any{"requests": requests})
}

func (s *Server) mlsDeviceJoinClaim(w http.ResponseWriter, r *http.Request, u user) {
	var in struct {
		RequestID string `json:"requestId"`
	}
	if decode(r, &in) != nil || in.RequestID == "" {
		groupBad(w, "缺少 requestId")
		return
	}
	record, oldRaw, err := s.readMLSDeviceJoin(r, in.RequestID)
	if err == pgx.ErrNoRows {
		writeJSON(w, 404, map[string]string{"error": "设备请求不存在"})
		return
	}
	if err != nil {
		dbError(w, err)
		return
	}
	if !s.isMLSGroupMember(r, record.GroupID, u.ID) {
		writeJSON(w, 403, map[string]string{"error": "非群成员"})
		return
	}
	now := time.Now().UTC()
	if !record.ExpiresAt.After(now) {
		writeJSON(w, 410, map[string]string{"error": "设备请求已过期"})
		return
	}
	if record.Status == "completed" {
		writeJSON(w, 409, map[string]string{"error": "设备请求已完成"})
		return
	}
	if record.Status == "claimed" && record.ClaimUntil != nil && record.ClaimUntil.After(now) {
		writeJSON(w, 409, map[string]string{"error": "设备请求正在处理"})
		return
	}
	until := now.Add(mlsDeviceClaimTTL)
	record.Status = "claimed"
	record.ClaimedBy = u.ID
	record.ClaimUntil = &until
	newRaw, _ := json.Marshal(record)
	tag, err := s.db.Exec(r.Context(), `UPDATE "SystemConfig" SET "value"=$3,"updatedAt"=NOW() WHERE "key"=$1 AND "value"=$2`, mlsDeviceKey(in.RequestID), oldRaw, string(newRaw))
	if err != nil {
		dbError(w, err)
		return
	}
	if tag.RowsAffected() != 1 {
		writeJSON(w, 409, map[string]string{"error": "设备请求已被其他客户端处理"})
		return
	}
	writeJSON(w, 200, map[string]any{"request": record})
}

func (s *Server) mlsDeviceJoinComplete(w http.ResponseWriter, r *http.Request, u user) {
	var in struct {
		RequestID         string          `json:"requestId"`
		Welcome           json.RawMessage `json:"welcome"`
		SenderIdentityKey string          `json:"senderIdentityKey"`
	}
	if decode(r, &in) != nil || in.RequestID == "" || len(in.Welcome) == 0 || in.SenderIdentityKey == "" {
		groupBad(w, "缺少 Welcome 参数")
		return
	}
	record, oldRaw, err := s.readMLSDeviceJoin(r, in.RequestID)
	if err == pgx.ErrNoRows {
		writeJSON(w, 404, map[string]string{"error": "设备请求不存在"})
		return
	}
	if err != nil {
		dbError(w, err)
		return
	}
	if record.Status != "claimed" || record.ClaimedBy != u.ID {
		writeJSON(w, 409, map[string]string{"error": "设备请求未由当前客户端认领"})
		return
	}
	now := time.Now().UTC()
	if !record.ExpiresAt.After(now) || record.ClaimUntil == nil || !record.ClaimUntil.After(now) {
		writeJSON(w, 410, map[string]string{"error": "设备请求认领已过期"})
		return
	}
	var welcome mlsWelcomeHeader
	if json.Unmarshal(in.Welcome, &welcome) != nil {
		groupBad(w, "Welcome 内容与设备请求不匹配")
		return
	}
	memberLeaf, hasMember := welcome.Members[record.MemberID]
	if welcome.GroupID != record.GroupID || welcome.Epoch < 0 || !hasMember || memberLeaf != welcome.LeafIndex || welcome.EncryptedGroupInfo.Ciphertext == "" || welcome.EncryptedGroupInfo.IV == "" {
		groupBad(w, "Welcome 内容与设备请求不匹配")
		return
	}
	record.Status = "completed"
	record.Welcome = in.Welcome
	record.SenderIdentityKey = in.SenderIdentityKey
	newRaw, _ := json.Marshal(record)
	tag, err := s.db.Exec(r.Context(), `UPDATE "SystemConfig" SET "value"=$3,"updatedAt"=NOW() WHERE "key"=$1 AND "value"=$2`, mlsDeviceKey(in.RequestID), oldRaw, string(newRaw))
	if err != nil {
		dbError(w, err)
		return
	}
	if tag.RowsAffected() != 1 {
		writeJSON(w, 409, map[string]string{"error": "设备请求状态已变化"})
		return
	}
	var publicState map[string]any
	if raw, found, _ := s.systemConfigValue(r.Context(), "mls:group:"+record.GroupID); found {
		_ = json.Unmarshal([]byte(raw), &publicState)
	}
	if publicState == nil {
		publicState = map[string]any{}
	}
	publicState["enabled"] = true
	publicState["epoch"] = welcome.Epoch
	publicState["members"] = welcome.Members
	publicState["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	if len(welcome.TreeSnapshot) > 0 {
		var tree any
		if json.Unmarshal(welcome.TreeSnapshot, &tree) == nil {
			publicState["treeSnapshot"] = tree
		}
	}
	stateRaw, _ := json.Marshal(publicState)
	if err = s.upsertSystemConfig(r.Context(), "mls:group:"+record.GroupID, string(stateRaw)); err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "epoch": welcome.Epoch})
}

func (s *Server) mlsDeviceJoinWelcome(w http.ResponseWriter, r *http.Request, u user) {
	groupID, deviceID := strings.TrimSpace(r.URL.Query().Get("groupId")), strings.TrimSpace(r.URL.Query().Get("deviceId"))
	if groupID == "" || deviceID == "" {
		groupBad(w, "缺少设备参数")
		return
	}
	requestID := mlsDeviceRequestID(groupID, u.ID, deviceID)
	record, _, err := s.readMLSDeviceJoin(r, requestID)
	if err == pgx.ErrNoRows {
		writeJSON(w, 200, map[string]bool{"ready": false})
		return
	}
	if err != nil {
		dbError(w, err)
		return
	}
	if record.UserID != u.ID {
		writeJSON(w, 200, map[string]bool{"ready": false})
		return
	}
	if record.Status != "completed" || len(record.Welcome) == 0 || record.SenderIdentityKey == "" {
		writeJSON(w, 200, map[string]any{"ready": false, "requestId": requestID})
		return
	}
	var welcome any
	_ = json.Unmarshal(record.Welcome, &welcome)
	writeJSON(w, 200, map[string]any{"ready": true, "requestId": requestID, "welcome": welcome, "senderIdentityKey": record.SenderIdentityKey})
}

func (s *Server) mlsDeviceJoinAck(w http.ResponseWriter, r *http.Request, u user) {
	var in struct {
		RequestID string `json:"requestId"`
	}
	if decode(r, &in) != nil || in.RequestID == "" {
		groupBad(w, "缺少 requestId")
		return
	}
	record, _, err := s.readMLSDeviceJoin(r, in.RequestID)
	if err == pgx.ErrNoRows || record.UserID != u.ID {
		writeJSON(w, 404, map[string]string{"error": "设备请求不存在"})
		return
	}
	if err != nil {
		dbError(w, err)
		return
	}
	if _, err = s.db.Exec(r.Context(), `DELETE FROM "SystemConfig" WHERE "key"=$1`, mlsDeviceKey(in.RequestID)); err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
