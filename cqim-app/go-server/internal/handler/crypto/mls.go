// Package crypto（mls.go）移植 server/mls-group.ts：MLS 群组端到端加密服务端支持。
//
// 服务端在 MLS 中扮演"哑管道"：存储分发 KeyPackage、转发 Welcome/Commit、
// 存储群组 MLS 元数据，不接触任何私钥或明文。
// 路由、JSON 字段、状态码、中文错误文案与 TS 版一致；全部路由受 userAuth 保护。
package crypto

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// ============================================================
// 1. KeyPackage 管理
// ============================================================

// POST /api/mls/upload-key-package
func (h *h) uploadKeyPackage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID     string `json:"userId"`
		KeyPackage any    `json:"keyPackage"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.UserID == "" || req.KeyPackage == nil {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}

	key := "mls:keypackage:" + req.UserID
	var packages []any
	if v, ok, err := h.cfgGet(r.Context(), key); err != nil {
		util.WriteError(w, 500, "上传失败")
		return
	} else if ok {
		_ = json.Unmarshal([]byte(v), &packages)
	}

	// 添加新的 KeyPackage（保留最近 10 个）
	packages = append(packages, req.KeyPackage)
	if len(packages) > 10 {
		packages = packages[len(packages)-10:]
	}

	if err := h.cfgUpsert(r.Context(), key, cfgJSON(packages)); err != nil {
		util.WriteError(w, 500, "上传失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"ok": true, "count": len(packages)})
}

// GET /api/mls/get-key-package?userId=xxx（消费一个）
func (h *h) getKeyPackage(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		util.WriteError(w, 400, "缺少 userId")
		return
	}
	key := "mls:keypackage:" + userID
	v, ok, err := h.cfgGet(r.Context(), key)
	if err != nil {
		util.WriteError(w, 500, "获取失败")
		return
	}
	if !ok {
		util.WriteError(w, 404, "未找到 KeyPackage")
		return
	}
	var packages []any
	if json.Unmarshal([]byte(v), &packages) != nil {
		util.WriteError(w, 404, "KeyPackage 数据损坏")
		return
	}
	if len(packages) == 0 {
		util.WriteError(w, 404, "无可用 KeyPackage")
		return
	}

	// 消费第一个 KeyPackage
	keyPackage := packages[0]
	packages = packages[1:]
	if err := h.cfgUpsert(r.Context(), key, cfgJSON(packages)); err != nil {
		util.WriteError(w, 500, "获取失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{
		"keyPackage": keyPackage,
		"remaining":  len(packages),
	})
}

// GET /api/mls/key-package-count?userId=xxx
func (h *h) keyPackageCount(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		util.WriteError(w, 400, "缺少 userId")
		return
	}
	count := 0
	if v, ok, err := h.cfgGet(r.Context(), "mls:keypackage:"+userID); err != nil {
		util.WriteError(w, 500, "查询失败")
		return
	} else if ok {
		var packages []any
		if json.Unmarshal([]byte(v), &packages) == nil {
			count = len(packages)
		}
	}
	util.WriteJSON(w, 200, map[string]any{"count": count})
}

// ============================================================
// 2. 群组 MLS 状态管理
// ============================================================

// groupMemberRole 查询群成员（用于权限校验）。
func (h *h) groupMemberRole(ctx context.Context, groupID, userID string) (*db.GroupMember, error) {
	m, err := db.QueryRowToStruct[db.GroupMember](ctx, h.d.DB,
		`SELECT "id","groupId","userId","role","nickname","lastAckSeq","joinTime","muteUntil","createdAt","updatedAt"
		 FROM "GroupMember" WHERE "groupId"=$1 AND "userId"=$2`, groupID, userID)
	if err != nil {
		if db.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return m, nil
}

// POST /api/mls/enable-group
func (h *h) enableGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID      string `json:"groupId"`
		UserID       string `json:"userId"`
		Epoch        *int   `json:"epoch"`
		TreeSnapshot any    `json:"treeSnapshot"`
		Members      any    `json:"members"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.UserID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}

	// 验证用户是群成员且有权限
	member, err := h.groupMemberRole(r.Context(), req.GroupID, req.UserID)
	if err != nil {
		util.WriteError(w, 500, "启用失败")
		return
	}
	if member == nil {
		util.WriteError(w, 403, "非群成员")
		return
	}

	epoch := 0
	if req.Epoch != nil {
		epoch = *req.Epoch
	}
	treeSnapshot := req.TreeSnapshot
	if treeSnapshot == nil {
		treeSnapshot = []any{}
	}
	members := req.Members
	if members == nil {
		members = map[string]any{}
	}
	mlsState := map[string]any{
		"enabled":      true,
		"epoch":        epoch,
		"creatorId":    req.UserID,
		"treeSnapshot": treeSnapshot,
		"members":      members,
		"enabledAt":    isoNow(),
		"updatedAt":    isoNow(),
	}
	if err := h.cfgUpsert(r.Context(), "mls:group:"+req.GroupID, cfgJSON(mlsState)); err != nil {
		util.WriteError(w, 500, "启用失败")
		return
	}

	resp := map[string]any{"ok": true}
	if req.Epoch != nil {
		resp["epoch"] = *req.Epoch
	}
	util.WriteJSON(w, 200, resp)
}

// GET /api/mls/group-state?groupId=xxx&userId=xxx
func (h *h) groupState(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	groupID := q.Get("groupId")
	userID := q.Get("userId")
	if groupID == "" {
		util.WriteError(w, 400, "缺少 groupId")
		return
	}

	// 验证是群成员
	if userID != "" {
		member, err := h.groupMemberRole(r.Context(), groupID, userID)
		if err != nil {
			util.WriteError(w, 500, "获取失败")
			return
		}
		if member == nil {
			util.WriteError(w, 403, "非群成员")
			return
		}
	}

	v, ok, err := h.cfgGet(r.Context(), "mls:group:"+groupID)
	if err != nil {
		util.WriteError(w, 500, "获取失败")
		return
	}
	if !ok {
		util.WriteJSON(w, 200, map[string]any{"enabled": false, "epochState": nil})
		return
	}
	var mlsState map[string]any
	_ = json.Unmarshal([]byte(v), &mlsState)
	util.WriteJSON(w, 200, map[string]any{
		"enabled":      mlsState["enabled"],
		"epoch":        mlsState["epoch"],
		"members":      mlsState["members"],
		"treeSnapshot": mlsState["treeSnapshot"],
		"enabledAt":    mlsState["enabledAt"],
	})
}

// POST /api/mls/update-group-state
func (h *h) updateGroupState(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID      string `json:"groupId"`
		Epoch        *int   `json:"epoch"`
		TreeSnapshot any    `json:"treeSnapshot"`
		Members      any    `json:"members"`
		CommitType   string `json:"commitType"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.Epoch == nil {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}

	key := "mls:group:" + req.GroupID
	var mlsState map[string]any
	if v, ok, err := h.cfgGet(r.Context(), key); err != nil {
		util.WriteError(w, 500, "更新失败")
		return
	} else if ok {
		_ = json.Unmarshal([]byte(v), &mlsState)
	}
	if mlsState == nil {
		mlsState = map[string]any{}
	}

	mlsState["epoch"] = *req.Epoch
	mlsState["updatedAt"] = isoNow()
	if req.TreeSnapshot != nil {
		mlsState["treeSnapshot"] = req.TreeSnapshot
	}
	if req.Members != nil {
		mlsState["members"] = req.Members
	}
	if req.CommitType != "" {
		mlsState["lastCommitType"] = req.CommitType
	}

	if err := h.cfgUpsert(r.Context(), key, cfgJSON(mlsState)); err != nil {
		util.WriteError(w, 500, "更新失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"ok": true, "epoch": *req.Epoch})
}

// GET /api/mls/is-enabled?groupId=xxx
func (h *h) isEnabled(w http.ResponseWriter, r *http.Request) {
	groupID := r.URL.Query().Get("groupId")
	if groupID == "" {
		util.WriteError(w, 400, "缺少 groupId")
		return
	}
	v, ok, _ := h.cfgGet(r.Context(), "mls:group:"+groupID)
	if !ok {
		util.WriteJSON(w, 200, map[string]any{"enabled": false})
		return
	}
	var mlsState map[string]any
	_ = json.Unmarshal([]byte(v), &mlsState)
	enabled := false
	if e, ok := mlsState["enabled"].(bool); ok {
		enabled = e
	}
	epoch := 0
	if e, ok := mlsState["epoch"].(float64); ok {
		epoch = int(e)
	}
	memberCount := 0
	if m, ok := mlsState["members"].(map[string]any); ok {
		memberCount = len(m)
	}
	util.WriteJSON(w, 200, map[string]any{
		"enabled":     enabled,
		"epoch":       epoch,
		"memberCount": memberCount,
	})
}

// ============================================================
// 3. MLS 消息转发（Welcome / Commit）
// ============================================================

// POST /api/mls/send-welcome
func (h *h) sendWelcome(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID           string `json:"groupId"`
		TargetUserID      string `json:"targetUserId"`
		Welcome           any    `json:"welcome"`
		SenderIdentityKey any    `json:"senderIdentityKey"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.TargetUserID == "" || req.Welcome == nil {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}

	// 存储 Welcome 消息（新成员上线后拉取）
	payload := map[string]any{
		"welcome":           req.Welcome,
		"senderIdentityKey": req.SenderIdentityKey,
		"createdAt":         isoNow(),
	}
	if err := h.cfgUpsert(r.Context(), "mls:welcome:"+req.GroupID+":"+req.TargetUserID, cfgJSON(payload)); err != nil {
		util.WriteError(w, 500, "存储失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"ok": true})
}

// GET /api/mls/pending-welcome?userId=xxx
func (h *h) pendingWelcome(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		util.WriteError(w, 400, "缺少 userId")
		return
	}
	configs, err := h.cfgByPrefix(r.Context(), "mls:welcome:")
	if err != nil {
		util.WriteError(w, 500, "获取失败")
		return
	}

	welcomes := []any{}
	for _, config := range configs {
		// key 格式: mls:welcome:{groupId}:{targetUserId}
		parts := strings.Split(config.Key, ":")
		if len(parts) >= 4 && parts[3] == userID {
			var data map[string]any
			if json.Unmarshal([]byte(config.Value), &data) == nil {
				entry := map[string]any{"groupId": parts[2]}
				for k, v := range data {
					entry[k] = v
				}
				welcomes = append(welcomes, entry)
			}
		}
	}
	util.WriteJSON(w, 200, map[string]any{"welcomes": welcomes})
}

// POST /api/mls/ack-welcome
func (h *h) ackWelcome(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID string `json:"groupId"`
		UserID  string `json:"userId"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.UserID == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}
	if err := h.cfgDelete(r.Context(), "mls:welcome:"+req.GroupID+":"+req.UserID); err != nil {
		util.WriteError(w, 500, "确认失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"ok": true})
}

// POST /api/mls/broadcast-commit
func (h *h) broadcastCommit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID    string         `json:"groupId"`
		Commit     map[string]any `json:"commit"`
		CommitType string         `json:"commitType"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.Commit == nil {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}

	commitType := req.CommitType
	if commitType == "" {
		commitType = "update"
	}
	// 存储最新的 Commit（覆盖旧的）
	epoch := jsonNumberToInt(req.Commit["epoch"])
	commitKey := "mls:commit:" + req.GroupID + ":" + strconv.Itoa(epoch)
	payload := map[string]any{
		"commit":     req.Commit,
		"commitType": commitType,
		"createdAt":  isoNow(),
	}
	if err := h.cfgUpsert(r.Context(), commitKey, cfgJSON(payload)); err != nil {
		util.WriteError(w, 500, "存储失败")
		return
	}

	// 清理旧的 Commit（只保留最近 50 个 epoch）
	oldEpoch := epoch - 50
	if oldEpoch > 0 {
		_ = h.cfgDelete(r.Context(), "mls:commit:"+req.GroupID+":"+strconv.Itoa(oldEpoch))
	}
	util.WriteJSON(w, 200, map[string]any{"ok": true, "epoch": epoch})
}

func jsonNumberToInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case string:
		if i, err := strconv.Atoi(n); err == nil {
			return i
		}
	}
	return 0
}

// GET /api/mls/pending-commits?groupId=xxx&afterEpoch=xxx
func (h *h) pendingCommits(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	groupID := q.Get("groupId")
	if groupID == "" {
		util.WriteError(w, 400, "缺少 groupId")
		return
	}
	configs, err := h.cfgByPrefix(r.Context(), "mls:commit:"+groupID+":")
	if err != nil {
		util.WriteError(w, 500, "获取失败")
		return
	}

	minEpoch := 0
	if after := q.Get("afterEpoch"); after != "" {
		if e, err := strconv.Atoi(after); err == nil {
			minEpoch = e
		}
	}

	type commitEntry struct {
		epoch int
		data  map[string]any
	}
	entries := []commitEntry{}
	for _, config := range configs {
		parts := strings.Split(config.Key, ":")
		epoch, err := strconv.Atoi(parts[len(parts)-1])
		if err != nil || epoch <= minEpoch {
			continue
		}
		var data map[string]any
		if json.Unmarshal([]byte(config.Value), &data) == nil {
			entries = append(entries, commitEntry{epoch: epoch, data: data})
		}
	}

	// 按 epoch 排序
	sort.Slice(entries, func(i, j int) bool { return entries[i].epoch < entries[j].epoch })
	commits := make([]any, 0, len(entries))
	for _, e := range entries {
		entry := map[string]any{"epoch": e.epoch}
		for k, v := range e.data {
			entry[k] = v
		}
		commits = append(commits, entry)
	}
	util.WriteJSON(w, 200, map[string]any{"commits": commits})
}

// ============================================================
// 4. 身份密钥管理
// ============================================================

// POST /api/mls/register-identity
func (h *h) registerIdentity(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID      string `json:"userId"`
		IdentityKey string `json:"identityKey"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.UserID == "" || req.IdentityKey == "" {
		util.WriteError(w, 400, "缺少必要参数")
		return
	}

	key := "mls:identity:" + req.UserID
	_, exists, err := h.cfgGet(r.Context(), key)
	if err != nil {
		util.WriteError(w, 500, "注册失败")
		return
	}
	now := isoNow()
	payload := map[string]string{"identityKey": req.IdentityKey, "updatedAt": now}
	if !exists {
		payload["createdAt"] = now
	}
	if err := h.cfgUpsert(r.Context(), key, cfgJSON(payload)); err != nil {
		util.WriteError(w, 500, "注册失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"ok": true})
}

// GET /api/mls/get-identity?userId=xxx
func (h *h) getIdentity(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		util.WriteError(w, 400, "缺少 userId")
		return
	}
	v, ok, err := h.cfgGet(r.Context(), "mls:identity:"+userID)
	if err != nil {
		util.WriteError(w, 500, "获取失败")
		return
	}
	if !ok {
		util.WriteError(w, 404, "未找到身份密钥")
		return
	}
	var data struct {
		IdentityKey string `json:"identityKey"`
	}
	_ = json.Unmarshal([]byte(v), &data)
	util.WriteJSON(w, 200, map[string]any{"identityKey": data.IdentityKey})
}

// POST /api/mls/batch-get-identity
func (h *h) batchGetIdentity(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserIDs []string `json:"userIds"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if len(req.UserIDs) == 0 {
		util.WriteError(w, 400, "缺少 userIds")
		return
	}

	keys := make([]string, len(req.UserIDs))
	for i, id := range req.UserIDs {
		keys[i] = "mls:identity:" + id
	}
	configs, err := db.QueryToStructs[db.SystemConfig](r.Context(), h.d.DB,
		`SELECT "key","value","updatedAt" FROM "SystemConfig" WHERE "key" = ANY($1)`, keys)
	if err != nil {
		util.WriteError(w, 500, "批量获取失败")
		return
	}

	result := map[string]string{}
	for _, config := range configs {
		userID := strings.TrimPrefix(config.Key, "mls:identity:")
		var data struct {
			IdentityKey string `json:"identityKey"`
		}
		if json.Unmarshal([]byte(config.Value), &data) == nil {
			result[userID] = data.IdentityKey
		}
	}
	util.WriteJSON(w, 200, map[string]any{"identityKeys": result})
}

// ============================================================
// 5. 统计与管理
// ============================================================

// GET /api/mls/stats?groupId=xxx
func (h *h) stats(w http.ResponseWriter, r *http.Request) {
	groupID := r.URL.Query().Get("groupId")
	if groupID == "" {
		util.WriteError(w, 400, "缺少 groupId")
		return
	}
	v, ok, err := h.cfgGet(r.Context(), "mls:group:"+groupID)
	if err != nil {
		util.WriteError(w, 500, "获取统计失败")
		return
	}
	if !ok {
		util.WriteJSON(w, 200, map[string]any{
			"enabled":     false,
			"epoch":       0,
			"memberCount": 0,
			"treeSize":    0,
		})
		return
	}
	var mlsState map[string]any
	_ = json.Unmarshal([]byte(v), &mlsState)
	enabled := false
	if e, ok := mlsState["enabled"].(bool); ok {
		enabled = e
	}
	epoch := jsonNumberToInt(mlsState["epoch"])
	memberCount := 0
	if m, ok := mlsState["members"].(map[string]any); ok {
		memberCount = len(m)
	}
	treeSize := 0
	if t, ok := mlsState["treeSnapshot"].([]any); ok {
		treeSize = len(t)
	}
	util.WriteJSON(w, 200, map[string]any{
		"enabled":        enabled,
		"epoch":          epoch,
		"memberCount":    memberCount,
		"treeSize":       treeSize,
		"enabledAt":      mlsState["enabledAt"],
		"updatedAt":      mlsState["updatedAt"],
		"lastCommitType": mlsState["lastCommitType"],
	})
}
