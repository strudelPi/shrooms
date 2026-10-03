#include "shrooms_agents.h"

#include <arpa/inet.h>
#include <cctype>
#include <cerrno>
#include <chrono>
#include <fstream>
#include <sstream>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fcntl.h>
#include <netinet/in.h>
#include <signal.h>
#include <spawn.h>
#include <sys/stat.h>
#include <sys/wait.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <unistd.h>

extern "C" char** environ;

namespace agents {

namespace {

constexpr size_t kMaxBody = 8 * 1024 * 1024;
constexpr size_t kKeepEvents = 4000;

std::string jsonEscape(const std::string& s)
{
    std::string out;
    out.reserve(s.size() + 2);
    for (unsigned char c : s) {
        switch (c) {
        case '"': out += "\\\""; break;
        case '\\': out += "\\\\"; break;
        case '\n': out += "\\n"; break;
        case '\r': out += "\\r"; break;
        case '\t': out += "\\t"; break;
        default:
            if (c < 0x20) {
                char b[8];
                std::snprintf(b, sizeof b, "\\u%04x", c);
                out += b;
            } else {
                out += static_cast<char>(c);
            }
        }
    }
    return out;
}

// Opens a TCP connection to a mesh address, or returns -1 with why. Both
// timeouts are set, so a peer that went away cannot hang a caller.
int dial(const std::string& address, int timeoutSec, std::string& err)
{
    if (!isMeshAddress(address)) {
        err = address + " is not a mesh address";
        return -1;
    }
    sockaddr_storage ss{};
    socklen_t len = 0;
    int family = AF_INET6;
    auto* v6 = reinterpret_cast<sockaddr_in6*>(&ss);
    auto* v4 = reinterpret_cast<sockaddr_in*>(&ss);
    if (inet_pton(AF_INET6, address.c_str(), &v6->sin6_addr) == 1) {
        v6->sin6_family = AF_INET6;
        v6->sin6_port = htons(kPort);
        len = sizeof(sockaddr_in6);
    } else {
        family = AF_INET;
        inet_pton(AF_INET, address.c_str(), &v4->sin_addr);
        v4->sin_family = AF_INET;
        v4->sin_port = htons(kPort);
        len = sizeof(sockaddr_in);
    }
    int fd = ::socket(family, SOCK_STREAM, 0);
    if (fd < 0) {
        err = std::string("socket: ") + std::strerror(errno);
        return -1;
    }
    timeval tv{};
    tv.tv_sec = timeoutSec;
    ::setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, sizeof tv);
    ::setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &tv, sizeof tv);
    if (::connect(fd, reinterpret_cast<sockaddr*>(&ss), len) < 0) {
        err = "cannot reach " + address + ": " + std::strerror(errno);
        ::close(fd);
        return -1;
    }
    return fd;
}

bool sendAll(int fd, const std::string& data, std::string& err)
{
    size_t sent = 0;
    while (sent < data.size()) {
        ssize_t n = ::send(fd, data.data() + sent, data.size() - sent, MSG_NOSIGNAL);
        if (n <= 0) {
            err = std::string("write: ") + std::strerror(errno);
            return false;
        }
        sent += static_cast<size_t>(n);
    }
    return true;
}

std::string requestText(const std::string& address, const std::string& method,
                        const std::string& target, const std::string& body, bool stream)
{
    std::string host = address.find(':') != std::string::npos ? "[" + address + "]" : address;
    // HTTP/1.0, so Go never chunks the reply. Over 1.1 an event stream comes
    // chunked, and a chunk boundary can fall in the middle of an event line.
    std::string req = method + " " + target + " HTTP/1.0\r\nHost: " + host + ":" +
                      std::to_string(kPort) + "\r\nConnection: close\r\n";
    if (stream) {
        req += "Accept: text/event-stream\r\n";
    }
    if (method != "GET") {
        req += "Content-Type: application/json\r\nContent-Length: " + std::to_string(body.size()) + "\r\n";
    }
    return req + "\r\n" + body;
}

// The agent's own error text from a JSON {"error": "..."} body, or the body.
std::string errorText(const std::string& body)
{
    auto k = body.find("\"error\":\"");
    if (k == std::string::npos) {
        return body.substr(0, 300);
    }
    auto start = k + 9;
    auto end = body.find('"', start);
    return body.substr(start, end == std::string::npos ? std::string::npos : end - start);
}

// Removes HTTP/1.1 chunked transfer framing, which Go uses for any reply it
// did not know the length of.
std::string unchunk(const std::string& in)
{
    std::string out;
    size_t i = 0;
    while (i < in.size()) {
        auto eol = in.find("\r\n", i);
        if (eol == std::string::npos) break;
        long n = std::strtol(in.substr(i, eol - i).c_str(), nullptr, 16);
        if (n <= 0) break;
        out.append(in, eol + 2, static_cast<size_t>(n));
        i = eol + 2 + static_cast<size_t>(n) + 2;
    }
    return out;
}

long long seqOf(const std::string& ev)
{
    auto k = ev.find("\"seq\":");
    return k == std::string::npos ? 0 : std::atoll(ev.c_str() + k + 6);
}

}  // namespace

bool safePath(const std::string& path)
{
    if (path.compare(0, 4, "/v1/") != 0 || path.size() > 512) return false;
    for (unsigned char c : path) {
        if (c <= 0x20 || c >= 0x7f) return false;
    }
    return path.find("..") == std::string::npos;
}

bool isMeshAddress(const std::string& address)
{
    unsigned char b[16];
    if (inet_pton(AF_INET6, address.c_str(), b) == 1) {
        return b[0] == 0xfd;
    }
    if (inet_pton(AF_INET, address.c_str(), b) == 1) {
        return b[0] == 198 && (b[1] == 18 || b[1] == 19);
    }
    return false;
}

bool request(const std::string& address, const std::string& method, const std::string& target,
             const std::string& body, int timeoutSec, std::string& out, std::string& err)
{
    int fd = dial(address, timeoutSec, err);
    if (fd < 0) return false;
    if (!sendAll(fd, requestText(address, method, target, body, false), err)) {
        ::close(fd);
        return false;
    }
    std::string raw;
    char buf[8192];
    for (;;) {
        ssize_t n = ::recv(fd, buf, sizeof buf, 0);
        if (n < 0) {
            err = std::string("read: ") + std::strerror(errno);
            ::close(fd);
            return false;
        }
        if (n == 0) break;
        raw.append(buf, static_cast<size_t>(n));
        if (raw.size() > kMaxBody) {
            err = "the reply is implausibly large";
            ::close(fd);
            return false;
        }
    }
    ::close(fd);
    auto sep = raw.find("\r\n\r\n");
    if (sep == std::string::npos || raw.compare(0, 5, "HTTP/") != 0) {
        err = "the reply is not HTTP";
        return false;
    }
    std::string headers = raw.substr(0, sep);
    std::string payload = raw.substr(sep + 4);
    for (auto& c : headers) c = static_cast<char>(std::tolower(static_cast<unsigned char>(c)));
    if (headers.find("transfer-encoding: chunked") != std::string::npos) {
        payload = unchunk(payload);
    }
    auto sp = raw.find(' ');
    std::string code = sp == std::string::npos ? "" : raw.substr(sp + 1, 3);
    if (code.empty() || code[0] != '2') {
        err = "the agent answered " + code + ": " + errorText(payload);
        return false;
    }
    out = payload;
    return true;
}

Hub::~Hub()
{
    stopFollower();
}

void Hub::find(const std::string& peers)
{
    bool expected = false;
    if (!finding_.compare_exchange_strong(expected, true)) return;

    std::vector<std::vector<std::string>> list;
    size_t i = 0;
    while (i <= peers.size()) {
        auto end = peers.find(';', i);
        std::string entry = peers.substr(i, end == std::string::npos ? std::string::npos : end - i);
        std::vector<std::string> f;
        size_t j = 0;
        while (f.size() < 3) {
            auto bar = entry.find('|', j);
            f.push_back(entry.substr(j, bar == std::string::npos ? std::string::npos : bar - j));
            if (bar == std::string::npos) break;
            j = bar + 1;
        }
        if (f.size() == 3 && isMeshAddress(f[2])) list.push_back(f);
        if (end == std::string::npos) break;
        i = end + 1;
    }

    std::thread([this, list]() {
        // One prober per peer, so a peer that does not answer costs its own
        // timeout and nobody else's.
        std::vector<std::thread> probes;
        for (const auto& p : list) {
            probes.emplace_back([this, p]() {
                std::string body, err;
                bool ok = request(p[2], "GET", "/v1/sessions", "", 3, body, err);
                std::lock_guard<std::mutex> g(mu_);
                if (ok) {
                    found_[p[2]] = "{\"name\":\"" + jsonEscape(p[0]) + "\",\"mesh\":\"" + jsonEscape(p[1]) +
                                   "\",\"address\":\"" + jsonEscape(p[2]) + "\",\"list\":" + body + "}";
                } else {
                    found_.erase(p[2]);
                }
            });
        }
        for (auto& t : probes) t.join();
        finding_ = false;
    }).detach();
}

std::string Hub::found()
{
    std::lock_guard<std::mutex> g(mu_);
    std::string out = "[";
    bool first = true;
    for (const auto& kv : found_) {
        if (!first) out += ",";
        out += kv.second;
        first = false;
    }
    return out + "]";
}

void Hub::stopFollower()
{
    generation_++;
    int fd = followFd_.exchange(-1);
    if (fd >= 0) ::shutdown(fd, SHUT_RDWR);
    if (follower_.joinable()) follower_.join();
}

void Hub::watch(const std::string& address, const std::string& session)
{
    stopFollower();
    {
        std::lock_guard<std::mutex> g(mu_);
        base_ += static_cast<long long>(events_.size());
        events_.clear();
        connected_ = false;
        error_.clear();
    }
    unsigned gen = generation_.load();
    follower_ = std::thread(&Hub::follow, this, address, session, gen);
}

void Hub::follow(std::string address, std::string session, unsigned generation)
{
    long long after = 0;
    while (generation_.load() == generation) {
        std::string err;
        // A long read timeout: the agent sends a comment every 20 seconds, so
        // 60 without anything is a dead connection, the normal way they end.
        int fd = dial(address, 60, err);
        if (fd >= 0) {
            followFd_ = fd;
            std::string target = "/v1/sessions/" + session + "/events?after=" + std::to_string(after);
            if (sendAll(fd, requestText(address, "GET", target, "", true), err)) {
                std::string buf;
                bool inBody = false;
                char chunk[8192];
                for (;;) {
                    ssize_t n = ::recv(fd, chunk, sizeof chunk, 0);
                    if (n <= 0) break;
                    buf.append(chunk, static_cast<size_t>(n));
                    if (!inBody) {
                        auto sep = buf.find("\r\n\r\n");
                        if (sep == std::string::npos) continue;
                        if (buf.compare(0, 5, "HTTP/") != 0 || buf.compare(8, 4, " 200") != 0) {
                            err = "the agent refused the stream: " + buf.substr(0, buf.find("\r\n"));
                            break;
                        }
                        buf.erase(0, sep + 4);
                        inBody = true;
                        std::lock_guard<std::mutex> g(mu_);
                        connected_ = true;
                        error_.clear();
                    }
                    // Lines of the stream. Chunk framing, when present, sits on
                    // lines of its own and is skipped as not being data.
                    size_t eol;
                    while ((eol = buf.find('\n')) != std::string::npos) {
                        std::string line = buf.substr(0, eol);
                        buf.erase(0, eol + 1);
                        if (!line.empty() && line.back() == '\r') line.pop_back();
                        if (line.compare(0, 6, "data: ") != 0) continue;
                        std::string ev = line.substr(6);
                        bool partial = ev.find("\"kind\":\"partial\"") != std::string::npos;
                        if (!partial) {
                            long long s = seqOf(ev);
                            if (s <= after) continue;
                            after = s;
                        }
                        std::lock_guard<std::mutex> g(mu_);
                        events_.push_back(ev);
                        if (events_.size() > kKeepEvents) {
                            size_t drop = events_.size() - kKeepEvents;
                            events_.erase(events_.begin(), events_.begin() + static_cast<long>(drop));
                            base_ += static_cast<long long>(drop);
                        }
                    }
                }
            }
            followFd_ = -1;
            ::close(fd);
        }
        {
            std::lock_guard<std::mutex> g(mu_);
            connected_ = false;
            if (!err.empty()) error_ = err;
        }
        for (int i = 0; i < 20 && generation_.load() == generation; i++) {
            std::this_thread::sleep_for(std::chrono::milliseconds(100));
        }
    }
}

std::string Hub::events(long long after)
{
    std::lock_guard<std::mutex> g(mu_);
    long long from = after < base_ ? base_ : after;
    std::string out = "{\"next\":" + std::to_string(base_ + static_cast<long long>(events_.size())) +
                      ",\"connected\":" + (connected_ ? "true" : "false") +
                      ",\"error\":\"" + jsonEscape(error_) + "\",\"events\":[";
    bool first = true;
    for (long long i = from - base_; i < static_cast<long long>(events_.size()); i++) {
        if (!first) out += ",";
        out += events_[static_cast<size_t>(i)];
        first = false;
    }
    return out + "]}";
}

namespace {

constexpr long long kMaxUpload = 50LL * 1024 * 1024;

std::string urlEncode(const std::string& s)
{
    static const char* hex = "0123456789ABCDEF";
    std::string out;
    for (unsigned char c : s) {
        if (std::isalnum(c) || c == '-' || c == '_' || c == '.' || c == '~') {
            out += static_cast<char>(c);
        } else {
            out += '%';
            out += hex[c >> 4];
            out += hex[c & 15];
        }
    }
    return out;
}

std::string baseName(const std::string& path)
{
    auto k = path.find_last_of('/');
    return k == std::string::npos ? path : path.substr(k + 1);
}

bool readFile(const std::string& path, std::string& out, std::string& err)
{
    struct stat st{};
    if (::stat(path.c_str(), &st) != 0 || !S_ISREG(st.st_mode)) {
        err = path + " is not a file";
        return false;
    }
    if (st.st_size > kMaxUpload) {
        err = "the file is larger than 50 MB";
        return false;
    }
    std::ifstream f(path, std::ios::binary);
    std::stringstream ss;
    ss << f.rdbuf();
    out = ss.str();
    if (out.empty()) {
        err = "the file is empty";
        return false;
    }
    return true;
}

}  // namespace

// A string field of a small JSON reply; enough for {"path":..,"text":..}.
std::string field(const std::string& json, const std::string& key)
{
    auto k = json.find("\"" + key + "\":\"");
    if (k == std::string::npos) return "";
    std::string out;
    for (size_t i = k + key.size() + 4; i < json.size(); i++) {
        char c = json[i];
        if (c == '"') break;
        if (c == '\\' && i + 1 < json.size()) {
            char n = json[++i];
            switch (n) {
            case 'n': out += '\n'; break;
            case 't': out += '\t'; break;
            case 'u': {
                // Go escapes <, > and & this way, and control characters:
                // decoded, so "A & B" comes back as said. A surrogate pair is
                // not combined; Go sends non-BMP text as UTF-8, not escaped.
                if (i + 4 >= json.size()) break;
                unsigned cp = static_cast<unsigned>(std::strtoul(json.substr(i + 1, 4).c_str(), nullptr, 16));
                i += 4;
                if (cp < 0x80) {
                    out += static_cast<char>(cp);
                } else if (cp < 0x800) {
                    out += static_cast<char>(0xC0 | (cp >> 6));
                    out += static_cast<char>(0x80 | (cp & 0x3F));
                } else {
                    out += static_cast<char>(0xE0 | (cp >> 12));
                    out += static_cast<char>(0x80 | ((cp >> 6) & 0x3F));
                    out += static_cast<char>(0x80 | (cp & 0x3F));
                }
                break;
            }
            default: out += n;
            }
        } else {
            out += c;
        }
    }
    return out;
}

long Hub::addJob(const std::string& kind, const std::string& name)
{
    std::lock_guard<std::mutex> g(mu_);
    Job j{nextJob_++, kind, "pending", name, "", "", ""};
    jobs_.push_back(j);
    if (jobs_.size() > 50) jobs_.erase(jobs_.begin());
    return j.id;
}

void Hub::finishJob(long id, bool ok, const std::string& path, const std::string& text, const std::string& error)
{
    std::lock_guard<std::mutex> g(mu_);
    for (auto& j : jobs_) {
        if (j.id != id) continue;
        j.state = ok ? "done" : "failed";
        j.path = path;
        j.text = text;
        j.error = error;
    }
}

long Hub::upload(const std::string& address, const std::string& session, const std::string& localPath)
{
    std::string name = baseName(localPath);
    long id = addJob("upload", name);
    std::thread([this, id, address, session, localPath, name]() {
        std::string body, out, err;
        if (!readFile(localPath, body, err)) {
            finishJob(id, false, "", "", err);
            return;
        }
        bool ok = request(address, "POST", "/v1/sessions/" + session + "/files?name=" + urlEncode(name),
                          body, 120, out, err);
        finishJob(id, ok, ok ? field(out, "path") : "", "", err);
    }).detach();
    return id;
}

std::string Hub::recordStart()
{
    std::lock_guard<std::mutex> g(mu_);
    if (recorder_ > 0) return "already recording";
    char tmpl[] = "/tmp/shrooms-voice-XXXXXX";
    int fd = ::mkstemp(tmpl);
    if (fd < 0) return std::string("mkstemp: ") + std::strerror(errno);
    ::close(fd);
    std::string path = std::string(tmpl) + ".wav";
    ::rename(tmpl, path.c_str());

    posix_spawn_file_actions_t fa;
    posix_spawn_file_actions_init(&fa);
    posix_spawn_file_actions_addopen(&fa, 1, "/dev/null", O_WRONLY, 0);
    posix_spawn_file_actions_addopen(&fa, 2, "/dev/null", O_WRONLY, 0);
    // Speech for a model that resamples to 16 kHz mono anyway.
    std::vector<std::vector<std::string>> tries = {
        {"pw-record", "--rate", "16000", "--channels", "1", path},
        {"parecord", "--file-format=wav", "--rate=16000", "--channels=1", path},
        {"arecord", "-q", "-f", "S16_LE", "-r", "16000", "-c", "1", path},
    };
    std::string why = "no recorder found (pw-record, parecord, arecord)";
    for (auto& t : tries) {
        std::vector<char*> argv;
        for (auto& a : t) argv.push_back(const_cast<char*>(a.c_str()));
        argv.push_back(nullptr);
        pid_t pid;
        int rc = posix_spawnp(&pid, argv[0], &fa, nullptr, argv.data(), environ);
        if (rc == 0) {
            recorder_ = pid;
            recording_ = path;
            posix_spawn_file_actions_destroy(&fa);
            return "";
        }
        why = std::string(argv[0]) + ": " + std::strerror(rc);
    }
    posix_spawn_file_actions_destroy(&fa);
    ::unlink(path.c_str());
    return why;
}

namespace {

// Stops a recorder the way it expects, so it finishes the file's header, and
// does not wait forever for it.
void stopRecorder(int pid)
{
    ::kill(pid, SIGINT);
    for (int i = 0; i < 30; i++) {
        if (::waitpid(pid, nullptr, WNOHANG) == pid) return;
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }
    ::kill(pid, SIGKILL);
    ::waitpid(pid, nullptr, 0);
}

}  // namespace

void Hub::recordCancel()
{
    int pid;
    std::string path;
    {
        std::lock_guard<std::mutex> g(mu_);
        pid = recorder_;
        path = recording_;
        recorder_ = -1;
        recording_.clear();
    }
    if (pid > 0) stopRecorder(pid);
    if (!path.empty()) ::unlink(path.c_str());
}

long Hub::recordStop(const std::string& address, const std::string& session, const std::string& lang,
                     std::string& err)
{
    int pid;
    std::string path;
    {
        std::lock_guard<std::mutex> g(mu_);
        pid = recorder_;
        path = recording_;
        recorder_ = -1;
        recording_.clear();
    }
    if (pid <= 0) {
        err = "not recording";
        return -1;
    }
    long id = addJob("voice", "voice note");
    std::thread([this, id, pid, path, address, session, lang]() {
        stopRecorder(pid);
        std::string body, out, err;
        bool ok = readFile(path, body, err);
        ::unlink(path.c_str());
        if (ok) {
            // A transcription takes a while on a laptop CPU: a minute of
            // speech is most of one.
            ok = request(address, "POST", "/v1/sessions/" + session + "/transcribe?name=voice.wav&lang=" +
                         urlEncode(lang), body, 300, out, err);
        }
        finishJob(id, ok, ok ? field(out, "path") : "", ok ? field(out, "text") : "", err);
    }).detach();
    return id;
}

namespace {

// Runs a command with its stdout to a file, waiting at most `secs`. Returns
// the exit status, or -1 if it could not be started (not installed).
int runTo(const std::vector<std::string>& cmd, const std::string& outPath, int secs)
{
    posix_spawn_file_actions_t fa;
    posix_spawn_file_actions_init(&fa);
    posix_spawn_file_actions_addopen(&fa, 1, outPath.c_str(), O_WRONLY | O_CREAT | O_TRUNC, 0600);
    posix_spawn_file_actions_addopen(&fa, 2, "/dev/null", O_WRONLY, 0);
    std::vector<char*> argv;
    for (auto& a : cmd) argv.push_back(const_cast<char*>(a.c_str()));
    argv.push_back(nullptr);
    pid_t pid;
    int rc = posix_spawnp(&pid, argv[0], &fa, nullptr, argv.data(), environ);
    posix_spawn_file_actions_destroy(&fa);
    if (rc != 0) return -1;
    int status = 0;
    for (int i = 0; i < secs * 20; i++) {
        if (::waitpid(pid, &status, WNOHANG) == pid) return WIFEXITED(status) ? WEXITSTATUS(status) : 1;
        std::this_thread::sleep_for(std::chrono::milliseconds(50));
    }
    ::kill(pid, SIGKILL);
    ::waitpid(pid, nullptr, 0);
    return 1;
}

std::string slurp(const std::string& path)
{
    std::ifstream f(path, std::ios::binary);
    std::stringstream ss;
    ss << f.rdbuf();
    return ss.str();
}

}  // namespace

long Hub::pasteImage(const std::string& address, const std::string& session, std::string& err)
{
    char tmpl[] = "/tmp/shrooms-paste-XXXXXX";
    int fd = ::mkstemp(tmpl);
    if (fd < 0) {
        err = std::string("mkstemp: ") + std::strerror(errno);
        return -1;
    }
    ::close(fd);
    std::string types = std::string(tmpl) + ".types";
    ::unlink(tmpl);

    // Which tool, and whether the clipboard holds an image at all: asked
    // first, so text is left to the ordinary paste.
    bool wayland = std::getenv("WAYLAND_DISPLAY") != nullptr;
    std::vector<std::string> list = wayland ? std::vector<std::string>{"wl-paste", "--list-types"}
                                            : std::vector<std::string>{"xclip", "-selection", "clipboard", "-t", "TARGETS", "-o"};
    int rc = runTo(list, types, 3);
    std::string have = slurp(types);
    ::unlink(types.c_str());
    if (rc == -1) {
        err = wayland ? "pasting images needs wl-paste: sudo apt install wl-clipboard"
                      : "pasting images needs xclip: sudo apt install xclip";
        return -1;
    }
    std::string mime;
    for (const char* m : {"image/png", "image/jpeg", "image/webp", "image/gif"}) {
        if (have.find(m) != std::string::npos) {
            mime = m;
            break;
        }
    }
    if (mime.empty()) return 0;

    std::vector<std::string> get = wayland ? std::vector<std::string>{"wl-paste", "--type", mime}
                                           : std::vector<std::string>{"xclip", "-selection", "clipboard", "-t", mime, "-o"};
    std::string ext = mime.substr(6);
    std::string file = std::string(tmpl) + "." + ext;
    if (runTo(get, file, 5) != 0) {
        ::unlink(file.c_str());
        err = "could not read the image from the clipboard";
        return -1;
    }
    long id = addJob("upload", "pasted." + ext);
    std::thread([this, id, address, session, file, ext]() {
        std::string body, out, e;
        bool ok = readFile(file, body, e);
        ::unlink(file.c_str());
        if (ok) {
            ok = request(address, "POST", "/v1/sessions/" + session + "/files?name=pasted." + ext, body, 120, out, e);
        }
        finishJob(id, ok, ok ? field(out, "path") : "", "", e);
    }).detach();
    return id;
}

std::string Hub::jobs()
{
    std::lock_guard<std::mutex> g(mu_);
    std::string out = std::string("{\"recording\":") + (recorder_ > 0 ? "true" : "false") + ",\"jobs\":[";
    for (size_t i = 0; i < jobs_.size(); i++) {
        const Job& j = jobs_[i];
        if (i) out += ",";
        out += "{\"id\":" + std::to_string(j.id) + ",\"kind\":\"" + j.kind + "\",\"state\":\"" + j.state +
               "\",\"name\":\"" + jsonEscape(j.name) + "\",\"path\":\"" + jsonEscape(j.path) +
               "\",\"text\":\"" + jsonEscape(j.text) + "\",\"error\":\"" + jsonEscape(j.error) + "\"}";
    }
    return out + "]}";
}

}  // namespace agents
