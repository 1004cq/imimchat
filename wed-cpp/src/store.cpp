#include "store.hpp"

#include <cstdlib>
#include <cstring>

#ifdef WED_HAS_PQ
#include <libpq-fe.h>
#endif
#ifdef WED_HAS_REDIS
#include <hiredis/hiredis.h>
#endif

namespace {

const char *envOr(const char *name, const char *fallback) {
  const char *v = std::getenv(name);
  return v && v[0] ? v : fallback;
}

}  // namespace

StoreStatus probeStores() {
  StoreStatus st;
#ifdef WED_HAS_PQ
  const char *url = std::getenv("DATABASE_URL");
  if (url && url[0]) {
    if (PGconn *c = PQconnectdb(url)) {
      if (PQstatus(c) == CONNECTION_OK) {
        if (PGresult *r = PQexec(c, "SELECT 1")) {
          st.postgres = PQresultStatus(r) == PGRES_TUPLES_OK;
          if (!st.postgres) st.postgresError = PQresultErrorMessage(r);
          PQclear(r);
        }
      } else {
        st.postgresError = PQerrorMessage(c);
      }
      PQfinish(c);
    }
  } else {
    st.postgresError = "DATABASE_URL missing";
  }
#else
  st.postgresError = "built without libpq";
#endif

#ifdef WED_HAS_REDIS
  const char *host = envOr("REDIS_HOST", "127.0.0.1");
  const int port = std::atoi(envOr("REDIS_PORT", "6379"));
  if (redisContext *c = redisConnect(host, port)) {
    if (!c->err) {
      if (redisReply *r = static_cast<redisReply *>(redisCommand(c, "PING"))) {
        st.redis = r->type == REDIS_REPLY_STATUS && r->str && std::strcmp(r->str, "PONG") == 0;
        freeReplyObject(r);
      }
    } else {
      st.redisError = c->errstr;
    }
    redisFree(c);
  }
#else
  st.redisError = "built without hiredis";
#endif
  return st;
}

bool redisSetPresence(const std::string &userId, const std::string &state,
                      const std::string &activeChatId) {
#ifdef WED_HAS_REDIS
  const char *host = envOr("REDIS_HOST", "127.0.0.1");
  const int port = std::atoi(envOr("REDIS_PORT", "6379"));
  redisContext *c = redisConnect(host, port);
  if (!c || c->err) {
    if (c) redisFree(c);
    return false;
  }
  const std::string key = "user:presence:" + userId;
  redisReply *r = static_cast<redisReply *>(
      redisCommand(c, "SETEX %s 90 %s", key.c_str(), state.c_str()));
  bool ok = r != nullptr;
  if (r) freeReplyObject(r);
  if (!activeChatId.empty()) {
    const std::string ck = "user:activeChat:" + userId;
    r = static_cast<redisReply *>(redisCommand(c, "SETEX %s 90 %s", ck.c_str(), activeChatId.c_str()));
    if (r) freeReplyObject(r);
  }
  redisFree(c);
  return ok;
#else
  (void)userId;
  (void)state;
  (void)activeChatId;
  return false;
#endif
}
