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
    // Programs the core starts get the environment without the AppImage's
    // loader settings: with them, coreutils refused to run and xdg-open failed.
    {
        setenv("LD_PRELOAD", "/tmp/.mount_x/usr/lib/libprocself_fix.so", 1);
        setenv("__BUNDLE_REAL_EXE", "/tmp/.mount_x/usr/bin/.logos_host.elf", 1);
        setenv("LD_LIBRARY_PATH", "/tmp/.mount_x/usr/lib", 1);
        auto e = childEnv();
        std::string all;
        for (auto& v : e.vars) all += v + "\n";
        CHECK(all.find("LD_PRELOAD=") == std::string::npos && all.find("__BUNDLE_REAL_EXE=") == std::string::npos &&
              all.find("LD_LIBRARY_PATH=") == std::string::npos, "loader settings passed on");
        CHECK(all.find("PATH=") != std::string::npos && e.ptrs.back() == nullptr, "the rest kept");
        unsetenv("LD_PRELOAD"); unsetenv("__BUNDLE_REAL_EXE"); unsetenv("LD_LIBRARY_PATH");
    }

    // Links the core opens for a view: web links only.
    CHECK(safeUrl("https://github.com/users/vpavlin/packages/container/shrooms-agent/settings"), "a plain https link");
    CHECK(safeUrl("http://vps.office.mesh:8099/x"), "a mesh http link");
    CHECK(!safeUrl("file:///etc/passwd"), "a file URL");
    CHECK(!safeUrl("javascript:alert(1)"), "a script URL");
    CHECK(!safeUrl("https://x.io/a b"), "a space, which xdg-open would split");
    CHECK(!safeUrl("https://x.io/\n--option"), "a newline");
    CHECK(!safeUrl("-https://x.io"), "an option");

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
    // Read to the end (the core answers in pieces); after that, only what
    // the session has said since — it may be this one, and talking.
    long long next = std::atoll(ev.c_str() + 8);
    for (int i = 0; i < 10000 && hub.events(next).find("\"more\":true") != std::string::npos; i++)
        next = std::atoll(hub.events(next).c_str() + 8);
    next = std::atoll(hub.events(next).c_str() + 8);
    std::string more = hub.events(next);
    CHECK(std::atoll(more.c_str() + 8) >= next, "next went backwards: %s", more.substr(0, 120).c_str());

    // The whole backlog, read in pieces: no reply much over half a megabyte,
    // and every event once, in order.
    {
        for (int i = 0; i < 50; i++) {
            std::this_thread::sleep_for(std::chrono::milliseconds(100));
            if (hub.events(0).find("\"more\":true") == std::string::npos && i > 10) break;
        }
        // The core keeps the last 4000 events, so a longer session starts
        // later than 1; from there, every event once, in order.
        long long at = 0, first = 0, last = 0, pieces = 0, overs = 0;
        size_t biggest = 0;
        bool ordered = true, more = true;
        while (more && pieces < 10000) {
            std::string r = hub.events(at);
            pieces++;
            biggest = std::max(biggest, r.size());
            more = r.find("\"more\":true") != std::string::npos;
            at = std::atoll(r.c_str() + 8);
            long long n = 0;
            // Each event starts {"seq":; inside a string it would be {\"seq\":.
            for (size_t p = r.find("{\"seq\":"); p != std::string::npos; p = r.find("{\"seq\":", p + 1)) {
                long long s = std::atoll(r.c_str() + p + 7);
                if (first == 0) first = s;
                else if (s != last + 1) ordered = false;
                last = s;
                n++;
            }
            // Over half a megabyte only when one event is that big by itself:
            // a reply is cut between events, never inside one.
            if (r.size() > 600 * 1024 && n > 1) overs++;
        }
        std::printf("     backlog: events %lld..%lld in %lld pieces, the biggest %zu bytes\n", first, last, pieces, biggest);
        CHECK(ordered && last > 0, "events skipped or repeated, %lld..%lld", first, last);
        CHECK(overs == 0, "%lld replies over the size with more than one event in them", overs);

        // Opened at its tail: the last five, nothing before (no copy kept of
        // it yet — with one, it opens at the copy).
        Hub::forgetHistory(addr, session);
        hub.watch(addr, session, -5);
        std::string t;
        for (int i = 0; i < 50; i++) {
            t = hub.events(0);
            if (t.find("\"seq\":") != std::string::npos) break;
            std::this_thread::sleep_for(std::chrono::milliseconds(100));
        }
        std::this_thread::sleep_for(std::chrono::milliseconds(500));
        t = hub.events(0);
        size_t p = t.find("{\"seq\":");
        long long tailFirst = p == std::string::npos ? -1 : std::atoll(t.c_str() + p + 7);
        CHECK(tailFirst >= last - 4 && tailFirst > 1, "the tail started at %lld of %lld", tailFirst, last);
    }

    // What is shown of a session is kept on disk, and shown — marked — when
    // its machine cannot be reached; replaced once it answers.
    {
        std::string kept = Hub::historyPath(addr, session);
        std::remove(kept.c_str());
        hub.watch(addr, session, 5);
        std::this_thread::sleep_for(std::chrono::milliseconds(1500));
        hub.watch(addr, "no-such-session", 5);  // leaving it keeps it, at the latest
        FILE* f = std::fopen(kept.c_str(), "r");
        int lines = 0;
        char buf[1 << 16];
        std::string firstLine;
        while (f && std::fgets(buf, sizeof buf, f)) {
            if (lines++ == 0) firstLine = buf;
        }
        if (f) std::fclose(f);
        CHECK(firstLine.compare(0, 9, "{\"saved\":") == 0 && lines >= 2 && lines <= 6,
              "kept: %d lines, first %s", lines, firstLine.c_str());

        // The same, as for a machine that does not answer.
        std::string away = Hub::historyPath("fd00::1", session);
        std::rename(kept.c_str(), away.c_str());
        hub.watch("fd00::1", session, 5);
        std::string r = hub.events(0);
        CHECK(r.find("\"connected\":false") != std::string::npos && r.find("\"kept\":0") == std::string::npos &&
              r.find("{\"seq\":") != std::string::npos, "offline: %s", r.substr(0, 160).c_str());
        // Opened at the whole history, nothing kept is shown: it is not what
        // was asked for.
        hub.watch("fd00::1", session, 0);
        r = hub.events(0);
        CHECK(r.find("{\"seq\":") == std::string::npos, "kept shown for the whole history");

        // Kept, then continued: the copy first, then only what came after it
        // — every event once, in order, from the copy's first.
        std::rename(away.c_str(), kept.c_str());
        hub.watch(addr, session, 5);
        r = hub.events(0);
        size_t p0 = r.find("{\"seq\":");
        long long keptFirst = p0 == std::string::npos ? -1 : std::atoll(r.c_str() + p0 + 7);
        CHECK(r.find("\"kept\":0") == std::string::npos && keptFirst > 0, "not shown kept first: %s", r.substr(0, 160).c_str());
        for (int i = 0; i < 50; i++) {
            r = hub.events(0);
            if (r.find("\"kept\":0") != std::string::npos) break;
            std::this_thread::sleep_for(std::chrono::milliseconds(100));
        }
        std::this_thread::sleep_for(std::chrono::milliseconds(500));
        r = hub.events(0);
        long long prev = 0, n = 0, firstSeen = 0;
        bool once = true;
        for (size_t q = r.find("{\"seq\":"); q != std::string::npos; q = r.find("{\"seq\":", q + 1)) {
            long long sq = std::atoll(r.c_str() + q + 7);
            if (firstSeen == 0) firstSeen = sq;
            if (prev && sq != prev + 1) once = false;
            prev = sq;
            n++;
        }
        CHECK(r.find("\"kept\":0") != std::string::npos && firstSeen == keptFirst && once && n >= 5,
              "not continued from the copy: first %lld (copy %lld), %lld events, in order %d: %s",
              firstSeen, keptFirst, n, once, r.substr(0, 160).c_str());

        Hub::forgetHistory(addr, session);
        CHECK(std::fopen(kept.c_str(), "r") == nullptr, "forgotten, still there");
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

    // The outbox, in a directory of its own (XDG_DATA_HOME): written to an
    // unreachable machine it waits, with the reason, across a restart of the
    // core; written to a reachable one it arrives, once.
    {
        char tmpl[] = "/tmp/outbox-check-XXXXXX";
        std::string data = ::mkdtemp(tmpl);
        setenv("XDG_DATA_HOME", data.c_str(), 1);
        {
            Hub h;
            std::string id = h.queueText("fd00::1", "nowhere", "written offline\twith a tab");
            std::string ob;
            for (int i = 0; i < 100; i++) {
                ob = h.outbox();
                if (ob.find("\"error\":\"\"") == std::string::npos) break;
                std::this_thread::sleep_for(std::chrono::milliseconds(100));
            }
            CHECK(ob.find(id) != std::string::npos && ob.find("written offline\\twith a tab") != std::string::npos &&
                  ob.find("\"error\":\"\"") == std::string::npos, "queued and failing: %s", ob.c_str());
        }
        {
            Hub h; // a restart: read back from disk
            std::string ob = h.outbox();
            CHECK(ob.find("written offline\\twith a tab") != std::string::npos, "lost across a restart: %s", ob.c_str());
            std::string id = ob.substr(ob.find("\"id\":\"") + 6);
            id = id.substr(0, id.find('"'));
            CHECK(h.unqueue(id) && h.outbox() == "[]", "cancel: %s", h.outbox().c_str());
        }
        // Delivered, to a session of its own on the agent (pi: no model call
        // needed for the message to be taken).
        std::string out, err;
        request(addr, "POST", "/v1/sessions", "{\"name\":\"outbox-check\",\"dir\":\"/tmp\",\"harness\":\"pi\"}", 5, out, err);
        {
            Hub h;
            std::string id = h.queueText(addr, "outbox-check", "from the outbox");
            for (int i = 0; i < 100 && h.outbox() != "[]"; i++) std::this_thread::sleep_for(std::chrono::milliseconds(100));
            CHECK(h.outbox() == "[]", "not sent: %s", h.outbox().c_str());
            request(addr, "GET", "/v1/sessions/outbox-check/history", "", 5, out, err);
            std::string ev;
            int fd = -1;
            (void)fd;
            bool ok = request(addr, "POST", "/v1/sessions/outbox-check/messages",
                              "{\"text\":\"from the outbox\",\"id\":\"" + id + "\"}", 5, ev, err);
            CHECK(ok && ev.find("\"duplicate\":true") != std::string::npos, "sent again was not a duplicate: %s %s",
                  ev.c_str(), err.c_str());
        }
        request(addr, "DELETE", "/v1/sessions/outbox-check", "", 5, out, err);
        std::string rm = "rm -rf " + data;
        (void)!std::system(rm.c_str());
        unsetenv("XDG_DATA_HOME");
    }

    std::printf(fails ? "\n%d FAILED\n" : "\nall passed\n", fails);
    return fails ? 1 : 0;
}
