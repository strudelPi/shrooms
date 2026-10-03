// Drives the core's agent hub against a real shrooms-agent, the way the view
// will: find, read, follow. Run by test/agents_check.sh; not part of the build.
#include "../core/src/shrooms_agents.h"

#include <algorithm>
#include <chrono>
#include <cstdio>
#include <cstdlib>
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

    hub.watch(addr, session, 0);
    std::string ev;
    for (int i = 0; i < 50; i++) {
        ev = hub.events(0);
        if (ev.find("\"seq\":") != std::string::npos) break;
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }
    CHECK(ev.find("\"connected\":true") != std::string::npos && ev.find("\"seq\":1,") != std::string::npos,
          "%s", ev.substr(0, 200).c_str());
    // Read to the end (the core answers in pieces); after that, nothing.
    long long next = std::atoll(ev.c_str() + 8);
    for (int i = 0; i < 10000 && hub.events(next).find("\"more\":true") != std::string::npos; i++)
        next = std::atoll(hub.events(next).c_str() + 8);
    next = std::atoll(hub.events(next).c_str() + 8);
    std::string more = hub.events(next);
    CHECK(more.find("\"events\":[]") != std::string::npos, "events after next: %s", more.substr(0, 120).c_str());

    // The whole backlog, read in pieces: no reply much over half a megabyte,
    // and every event once, in order.
    {
        for (int i = 0; i < 50; i++) {
            std::this_thread::sleep_for(std::chrono::milliseconds(100));
            if (hub.events(0).find("\"more\":true") == std::string::npos && i > 10) break;
        }
        long long at = 0, last = 0, pieces = 0;
        size_t biggest = 0;
        bool ordered = true, more = true;
        while (more && pieces < 10000) {
            std::string r = hub.events(at);
            pieces++;
            biggest = std::max(biggest, r.size());
            more = r.find("\"more\":true") != std::string::npos;
            at = std::atoll(r.c_str() + 8);
            // Each event starts {"seq":; inside a string it would be {\"seq\":.
            for (size_t p = r.find("{\"seq\":"); p != std::string::npos; p = r.find("{\"seq\":", p + 1)) {
                long long s = std::atoll(r.c_str() + p + 7);
                if (s != last + 1) ordered = false;
                last = s;
            }
        }
        std::printf("     backlog: %lld events in %lld pieces, the biggest %zu bytes\n", last, pieces, biggest);
        CHECK(ordered && last > 0, "events skipped or repeated, last %lld", last);
        CHECK(biggest < 2 * 512 * 1024 || pieces == 1, "a reply of %zu bytes", biggest);

        // Opened at its tail: the last five, nothing before.
        hub.watch(addr, session, 5);
        std::string t;
        for (int i = 0; i < 50; i++) {
            t = hub.events(0);
            if (t.find("\"seq\":") != std::string::npos) break;
            std::this_thread::sleep_for(std::chrono::milliseconds(100));
        }
        std::this_thread::sleep_for(std::chrono::milliseconds(500));
        t = hub.events(0);
        size_t p = t.find("{\"seq\":");
        long long first = p == std::string::npos ? -1 : std::atoll(t.c_str() + p + 7);
        CHECK(first >= last - 4 && first > 1, "the tail started at %lld of %lld", first, last);
    }

    // Go escapes <, > and & as \u sequences: they must come back as written.
    CHECK(field("{\"text\":\"A \\u0026 B \\u003cx\\u003e \\\"q\\\" Vašek\"}", "text") == "A & B <x> \"q\" Vašek",
          "%s", field("{\"text\":\"A \\u0026 B \\u003cx\\u003e\"}", "text").c_str());

    // A file, in the background, to the session's machine.
    std::string local = "/tmp/agents-check-upload.txt";
    { FILE* f = std::fopen(local.c_str(), "w"); std::fputs("hello from the check\n", f); std::fclose(f); }
    long id = hub.upload(addr, session, local);
    std::string jobs;
    for (int i = 0; i < 50; i++) {
        jobs = hub.jobs();
        if (jobs.find("\"state\":\"pending\"") == std::string::npos) break;
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }
    CHECK(id > 0 && jobs.find("\"state\":\"done\"") != std::string::npos && jobs.find("/uploads/" + session + "/") != std::string::npos,
          "%s", jobs.c_str());
    std::remove(local.c_str());
    hub.upload(addr, session, "/nonexistent");
    std::this_thread::sleep_for(std::chrono::milliseconds(300));
    CHECK(hub.jobs().find("is not a file") != std::string::npos, "%s", hub.jobs().c_str());

    // Pasting an image: says what to install when the clipboard tool is
    // missing, and reports no image (so text pastes as usual) when there is
    // none — never a silent nothing.
    {
        std::string perr;
        long pid = hub.pasteImage(addr, session, perr);
        CHECK(pid == 0 || (pid < 0 && perr.find("sudo apt install") != std::string::npos) || pid > 0,
              "paste: %ld %s", pid, perr.c_str());
        std::printf("     paste: %s\n", pid > 0 ? "an image went" : (pid == 0 ? "no image on the clipboard" : perr.c_str()));
    }

    // A voice note: two seconds from the real microphone, transcribed on the
    // agent's machine. Only whether it went through is checked; what the room
    // said is not printed.
    if (std::getenv("AGENTS_CHECK_VOICE")) {
        std::string why = hub.recordStart();
        CHECK(why.empty() && hub.jobs().find("\"recording\":true") != std::string::npos, "%s", why.c_str());
        std::this_thread::sleep_for(std::chrono::seconds(2));
        std::string err;
        long vid = hub.recordStop(addr, session, "en", err);
        CHECK(vid > 0, "%s", err.c_str());
        for (int i = 0; i < 300; i++) {
            jobs = hub.jobs();
            if (jobs.find("\"kind\":\"voice\",\"state\":\"pending\"") == std::string::npos) break;
            std::this_thread::sleep_for(std::chrono::milliseconds(100));
        }
        CHECK(jobs.find("\"kind\":\"voice\",\"state\":\"done\"") != std::string::npos &&
              jobs.find("voice.wav") != std::string::npos && jobs.find("\"recording\":false") != std::string::npos,
              "voice job did not finish: %s", jobs.substr(jobs.find("voice") == std::string::npos ? 0 : jobs.find("voice")).substr(0, 200).c_str());
    }

    std::printf(fails ? "\n%d FAILED\n" : "\nall passed\n", fails);
    return fails ? 1 : 0;
}
