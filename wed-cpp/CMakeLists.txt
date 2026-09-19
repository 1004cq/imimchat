cmake_minimum_required(VERSION 3.16)
project(wed_cpp VERSION 0.3.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

add_executable(wed_cpp
  src/main.cpp
  src/http_min.cpp
  src/store.cpp
  src/auth.cpp
)

target_include_directories(wed_cpp PRIVATE include)
target_compile_options(wed_cpp PRIVATE -Wall -Wextra -Wpedantic)
find_package(OpenSSL REQUIRED)
target_link_libraries(wed_cpp PRIVATE OpenSSL::Crypto)

find_library(PQ_LIB pq)
find_path(PQ_INC libpq-fe.h PATH_SUFFIXES postgresql pgsql)
if(PQ_LIB AND PQ_INC)
  target_compile_definitions(wed_cpp PRIVATE WED_HAS_PQ=1)
  target_include_directories(wed_cpp PRIVATE ${PQ_INC})
  target_link_libraries(wed_cpp PRIVATE ${PQ_LIB})
endif()

find_library(HIREDIS_LIB hiredis)
find_path(HIREDIS_INC hiredis/hiredis.h)
if(HIREDIS_LIB AND HIREDIS_INC)
  target_compile_definitions(wed_cpp PRIVATE WED_HAS_REDIS=1)
  target_include_directories(wed_cpp PRIVATE ${HIREDIS_INC})
  target_link_libraries(wed_cpp PRIVATE ${HIREDIS_LIB})
endif()
