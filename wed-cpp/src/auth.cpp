#include "auth.hpp"

#include <openssl/evp.h>
#include <openssl/hmac.h>

#include <cstdlib>
#include <cstring>
#include <sstream>
#include <vector>

#ifdef WED_HAS_PQ
#include <libpq-fe.h>
#endif

namespace {

std::string trim(std::string s) {
  while (!s.empty() && (s.front() == ' ' || s.front() == '\t')) s.erase(s.begin());
  return s;
}

std::string bearer(const std::string &authorization) {
  auto v = trim(authorization);
  const char *p = "Bearer ";
  if (v.size() > 7 && v.compare(0, 7, p) == 0) return trim(v.substr(7));
  return v;
}

std::string b64urlDecode(std::string in) {
  for (char &c : in) {
    if (c == '-') c = '+';
    else if (c == '_') c = '/';
  }
  while (in.size() % 4) in.push_back('=');
  static const unsigned char tbl[256] = {
      0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,
      0,0,0,0,0,0,0,0,0,0,0,62,0,0,0,63,52,53,54,55,56,57,58,59,60,61,0,0,0,0,0,0,
      0,0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,0,0,0,0,0,
      0,26,27,28,29,30,31,32,33,34,35,36,37,38,39,40,41,42,43,44,45,46,47,48,49,50,51};
  std::string out;
  unsigned int val = 0;
  int valb = -8;
  for (unsigned char c : in) {
    if (c == '=') break;
    unsigned char d = tbl[c];
    val = (val << 6) + d;
    valb += 6;
    if (valb >= 0) {
      out.push_back(char((val >> valb) & 0xFF));
      valb -= 8;
    }
  }
  return out;
}

std::string jsonField(const std::string &json, const std::string &key) {
  const std::string pat = "\"" + key + "\"";
  auto p = json.find(pat);
  if (p == std::string::npos) return {};
  p = json.find(':', p);
  if (p == std::string::npos) return {};
  p = json.find_first_not_of(" \t\n\r", p + 1);
  if (p == std::string::npos || json[p] != '"') return {};
  const auto e = json.find('"', p + 1);
  return e == std::string::npos ? std::string{} : json.substr(p + 1, e - p - 1);
}

bool hmacEqual(const std::string &a, const std::string &b) {
  if (a.size() != b.size()) return false;
  unsigned char acc = 0;
  for (size_t i = 0; i < a.size(); ++i) acc |= static_cast<unsigned char>(a[i] ^ b[i]);
  return acc == 0;
}

std::optional<AuthUser> verifyJwt(const std::string &token) {
  const char *secret = std::getenv("JWT_SECRET");
  if (!secret || !secret[0]) secret = std::getenv("JWT_SECRET_KEY");
  if (!secret || !secret[0]) return std::nullopt;

  const auto d1 = token.find('.');
  const auto d2 = token.find('.', d1 == std::string::npos ? 0 : d1 + 1);
  if (d1 == std::string::npos || d2 == std::string::npos) return std::nullopt;
  const std::string signing = token.substr(0, d2);
  const std::string sigB64 = token.substr(d2 + 1);
  const std::string sig = b64urlDecode(sigB64);

  unsigned char mac[EVP_MAX_MD_SIZE];
  unsigned int macLen = 0;
  HMAC(EVP_sha256(), secret, static_cast<int>(std::strlen(secret)),
       reinterpret_cast<const unsigned char *>(signing.data()), signing.size(), mac, &macLen);
  if (!hmacEqual(std::string(reinterpret_cast<char *>(mac), macLen), sig)) return std::nullopt;

  const auto payload = b64urlDecode(token.substr(d1 + 1, d2 - d1 - 1));
  auto id = jsonField(payload, "userId");
  if (id.empty()) id = jsonField(payload, "id");
  if (id.empty()) id = jsonField(payload, "sub");
  if (id.empty()) return std::nullopt;
  return AuthUser{id, "jwt"};
}

std::optional<AuthUser> verifySession(const std::string &token) {
#ifdef WED_HAS_PQ
  const char *url = std::getenv("DATABASE_URL");
  if (!url || !url[0]) return std::nullopt;
  PGconn *c = PQconnectdb(url);
  if (!c || PQstatus(c) != CONNECTION_OK) {
    if (c) PQfinish(c);
    return std::nullopt;
  }
  const char *vals[1] = {token.c_str()};
  PGresult *r = PQexecParams(
      c,
      "SELECT \"userId\" FROM \"UserSession\" WHERE token = $1 AND \"expiresAt\" > NOW() LIMIT 1",
      1, nullptr, vals, nullptr, nullptr, 0);
  std::optional<AuthUser> out;
  if (r && PQresultStatus(r) == PGRES_TUPLES_OK && PQntuples(r) == 1) {
    out = AuthUser{PQgetvalue(r, 0, 0), "session"};
  }
  if (r) PQclear(r);
  PQfinish(c);
  return out;
#else
  (void)token;
  return std::nullopt;
#endif
}

}  // namespace

std::optional<AuthUser> authenticate(const std::string &authorization) {
  const auto token = bearer(authorization);
  if (token.empty()) return std::nullopt;
  if (auto jwt = verifyJwt(token)) return jwt;
  return verifySession(token);
}
