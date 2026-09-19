#include "http_min.hpp"

#include <arpa/inet.h>
#include <netinet/in.h>
#include <sys/socket.h>
#include <unistd.h>

#include <cstring>
#include <sstream>
#include <thread>
#include <vector>

namespace {

std::string lower(std::string s) {
  for (char &c : s) if (c >= 'A' && c <= 'Z') c = static_cast<char>(c + 32);
  return s;
}

HttpReq parseRequest(const std::string &raw) {
  HttpReq req;
  const auto headEnd = raw.find("\r\n\r\n");
  const std::string head = raw.substr(0, headEnd == std::string::npos ? raw.size() : headEnd);
  std::istringstream hs(head);
  std::string line;
  if (std::getline(hs, line)) {
    if (!line.empty() && line.back() == '\r') line.pop_back();
    std::istringstream ls(line);
    ls >> req.method >> req.path;
  }
  while (std::getline(hs, line)) {
    if (!line.empty() && line.back() == '\r') line.pop_back();
    const auto colon = line.find(':');
    if (colon == std::string::npos) continue;
    req.headers[lower(line.substr(0, colon))] = line.substr(colon + 1);
  }
  if (headEnd != std::string::npos) req.body = raw.substr(headEnd + 4);
  return req;
}

std::string serialize(const HttpRes &res) {
  std::ostringstream o;
  o << "HTTP/1.1 " << res.status << " \r\n"
    << "Content-Type: " << res.contentType << "\r\n"
    << "Content-Length: " << res.body.size() << "\r\n"
    << "Cache-Control: no-store\r\n"
    << "Connection: close\r\n\r\n"
    << res.body;
  return o.str();
}

}  // namespace

void httpServe(const std::string &bind, uint16_t port,
               const std::unordered_map<std::string, HttpHandler> &routes) {
  const int fd = ::socket(AF_INET, SOCK_STREAM, 0);
  if (fd < 0) return;
  int yes = 1;
  setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &yes, sizeof(yes));
  sockaddr_in addr{};
  addr.sin_family = AF_INET;
  addr.sin_port = htons(port);
  inet_pton(AF_INET, bind.c_str(), &addr.sin_addr);
  if (bind(fd, reinterpret_cast<sockaddr *>(&addr), sizeof(addr)) < 0) {
    close(fd);
    return;
  }
  listen(fd, 64);
  for (;;) {
    const int cfd = accept(fd, nullptr, nullptr);
    if (cfd < 0) continue;
    std::thread([cfd, routes] {
      std::vector<char> buf(65536);
      const ssize_t n = recv(cfd, buf.data(), buf.size(), 0);
      if (n > 0) {
        const auto req = parseRequest(std::string(buf.data(), static_cast<size_t>(n)));
        const std::string key = req.method + " " + req.path;
        HttpRes res;
        const auto it = routes.find(key);
        if (it != routes.end()) res = it->second(req);
        else {
          res.status = 404;
          res.body = R"({"error":"not_found"})";
        }
        const auto out = serialize(res);
        send(cfd, out.data(), out.size(), 0);
      }
      close(cfd);
    }).detach();
  }
}
