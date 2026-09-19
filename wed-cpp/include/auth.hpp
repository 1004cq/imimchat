#pragma once
#include <optional>
#include <string>

struct AuthUser {
  std::string id;
  std::string source;  // jwt | session
};

std::optional<AuthUser> authenticate(const std::string &authorization);
