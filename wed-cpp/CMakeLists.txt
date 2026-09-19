cmake_minimum_required(VERSION 3.16)
project(wed_cpp VERSION 0.2.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

add_executable(wed_cpp
  src/main.cpp
  src/http_min.cpp
  src/store.cpp
)

target_include_directories(wed_cpp PRIVATE include)
target_compile_options(wed_cpp PRIVATE -Wall -Wextra -Wpedantic)

find_package(Drogon 1.9.13 CONFIG QUIET)
if(Drogon_FOUND)
  target_compile_definitions(wed_cpp PRIVATE WED_HAS_DROGON=1)
  target_link_libraries(wed_cpp PRIVATE Drogon::Drogon)
endif()

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

message(STATUS "Drogon=${Drogon_FOUND} libpq=${PQ_LIB} hiredis=${HIREDIS_LIB}")
