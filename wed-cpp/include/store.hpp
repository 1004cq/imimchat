#pragma once
#include <string>

struct StoreStatus {
  bool postgres = false;
  bool redis = false;
  std::string postgresError;
  std::string redisError;
};

StoreStatus probeStores();
bool redisSetPresence(const std::string &userId, const std::string &state,
                      const std::string &activeChatId);
