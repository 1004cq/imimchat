cmake_minimum_required(VERSION 3.24)
project(wed_cpp VERSION 0.1.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

# Drogon provides the async HTTP/WebSocket runtime and optional PostgreSQL ORM.
# Install Drogon v1.9.13 on the build host; do not vendor credentials or keys.
find_package(Drogon 1.9.13 CONFIG REQUIRED)

add_executable(wed_cpp
  src/main.cpp
)

target_include_directories(wed_cpp PRIVATE include)
target_link_libraries(wed_cpp PRIVATE Drogon::Drogon)
target_compile_options(wed_cpp PRIVATE -Wall -Wextra -Wpedantic -Werror)
