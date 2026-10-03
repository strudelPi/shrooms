// Drives the core's agent hub against a real shrooms-agent, the way the view
// will: find, read, follow. Run by test/agents_check.sh; not part of the build.
#include "../core/src/shrooms_agents.h"

#include <chrono>
#include <cstdio>
#include <cstring>
#include <thread>

using namespace agents;

static int fails = 0;
#define CHECK(c, ...) do { if (!(c)) { std::printf("FAIL %s: ", #c); std::printf(__VA_ARGS__); std::printf("\n"); fails++; } else std::printf("ok   %s\n", #c); } while (0)

int main(int argc, char** argv)
{
    if (argc < 3) { std::printf("usage: agents_check <address> <session>\n"); return 2; }
    std::string addr = argv[1], session = argv[2];

    CHECK(isMeshAddress(addr), "%s", addr.c_str());
    CHECK(!isMeshAddress("128.140.55.128"), "public address accepted");
    CHECK(!isMeshAddress("laptop.office.mesh"), "a name accepted");
    CHECK(isMeshAddress("198.19.245.139"), "alias refused");
    CHECK(!safePath("/v1/../../etc"), "dotdot");
    CHECK(!safePath("/v1/x HTTP/1.0\r\nX: y"), "header injection");
    CHECK(!safePath("/admin"), "outside the API");

    std::string out, err;
    bool ok = request(addr, "GET", "/v1/sessions", "", 5, out, err);
    CHECK(ok && out.find("\"sessions\"") != std::string::npos, "%s %s", err.c_str(), out.substr(0, 80).c_str());
    ok = request(addr, "GET", "/v1/sessions/no-such-session/history", "", 5, out, err);
    CHECK(!ok && err.find("404") != std::string::npos && err.find("no session") != std::string::npos,
          "error: %s", err.c_str());
    ok = request("fd00::1", "GET", "/v1/sessions", "", 1, out, err);
    CHECK(!ok, "an unreachable agent answered");

    Hub hub;
    hub.find("laptop|office|" + addr + ";bogus|office|fd00::1;public|x|8.8.8.8");
    std::string found;
    for (int i = 0; i < 50; i++) {
        found = hub.found();
        if (found.find(addr) != std::string::npos) break;
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }
    CHECK(found.find("\"name\":\"laptop\"") != std::string::npos && found.find("\"list\":{") != std::string::npos,
          "%s", found.substr(0, 200).c_str());
    CHECK(found.find("8.8.8.8") == std::string::npos, "a public address was probed");

    hub.watch(addr, session);
    std::string ev;
    for (int i = 0; i < 50; i++) {
        ev = hub.events(0);
        if (ev.find("\"seq\":") != std::string::npos) break;
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }
    CHECK(ev.find("\"connected\":true") != std::string::npos && ev.find("\"seq\":1,") != std::string::npos,
          "%s", ev.substr(0, 200).c_str());
    long long next = std::atoll(ev.c_str() + 8);
    std::string more = hub.events(next);
    CHECK(more.find("\"events\":[]") != std::string::npos, "events after next: %s", more.substr(0, 120).c_str());

    std::printf(fails ? "\n%d FAILED\n" : "\nall passed\n", fails);
    return fails ? 1 : 0;
}
