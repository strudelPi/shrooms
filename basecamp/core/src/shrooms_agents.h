#pragma once

#include <atomic>
#include <map>
#include <mutex>
#include <string>
#include <thread>
#include <vector>

/**
 * Agents on the mesh (docs/agents.md), for the Basecamp view.
 *
 * The view runs in Basecamp's QML sandbox, which blocks all network access,
 * and reaches the outside through synchronous calls into this module. A slow
 * call freezes the whole view, so the network is kept off that path: finding
 * agents and following a session's live stream happen on threads of their own
 * here, and the view's calls only read what they have collected. Sending a
 * message, answering a prompt and changing a setting are single requests to a
 * machine on the mesh, bounded by a short timeout.
 *
 * Only mesh addresses are ever dialled (ULA fd00::/8 and the 198.18.0.0/15
 * IPv4 aliases), on the agent port: the connections are plain HTTP and rely
 * on the WireGuard tunnel for encryption and for who can reach an agent.
 *
 * Qt-free and ASCII-only, as the module glue requires.
 */
namespace agents {

constexpr int kPort = 7387;

/**
 * Whether a path may be sent to an agent: its API only, printable, no spaces
 * and no way to step out of it.
 */
bool safePath(const std::string& path);

/** A string field of a small JSON reply, with its escapes decoded. */
std::string field(const std::string& json, const std::string& key);

/** Whether an address is a literal mesh address. */
bool isMeshAddress(const std::string& address);

/**
 * One plain HTTP request to an agent. Returns true on a 2xx, with the body;
 * otherwise false and why, including the agent's own error text.
 */
bool request(const std::string& address, const std::string& method, const std::string& target,
             const std::string& body, int timeoutSec, std::string& out, std::string& err);

class Hub {
public:
    ~Hub();

    /**
     * Probes the given peers for agents in the background. `peers` is
     * "name|mesh|address" entries separated by ";". A round already running
     * is not started again.
     */
    void find(const std::string& peers);

    /** What the last rounds found, as a JSON array. */
    std::string found();

    /**
     * Follows one session's live stream, replacing whatever was followed.
     * tail > 0 starts at the last `tail` events instead of the first.
     */
    void watch(const std::string& address, const std::string& session, int tail);

    /**
     * The watched session's events after a local index, as
     * {"next":N,"connected":bool,"error":"...","events":[...]}. Events are the
     * agent's own JSON, verbatim; "partial" ones carry streamed reply text.
     */
    std::string events(long long after);

    /**
     * Sends a local file to a session's machine in the background (an agent
     * keeps it and returns its path there). Returns the job's id.
     */
    long upload(const std::string& address, const std::string& session, const std::string& localPath);

    /**
     * Starts recording a voice note from the default microphone, with the
     * system's own recorder (pw-record, else parecord, else arecord): Basecamp
     * ships no Qt Multimedia, and a view that imports a missing module does
     * not load at all. Returns an empty string or why it could not start.
     */
    std::string recordStart();

    /**
     * Stops recording and, in the background, sends the note to be
     * transcribed on that machine (whisper.cpp, docs/agents.md). Returns the
     * job's id, or -1 with why in err. cancel drops the recording.
     */
    long recordStop(const std::string& address, const std::string& session, const std::string& lang,
                    std::string& err);
    void recordCancel();

    /**
     * An image on the clipboard, sent like a file: Basecamp's QML can paste
     * text only. Read with wl-paste (Wayland) or xclip (X11). Returns the
     * job's id, 0 when the clipboard holds no image (the caller pastes its
     * text as usual), or -1 with why in err.
     */
    long pasteImage(const std::string& address, const std::string& session, std::string& err);

    /**
     * Background jobs, as {"recording":bool,"jobs":[{"id","kind","state",
     * "name","path","text","error"}]}. state is pending, done or failed.
     */
    std::string jobs();

private:
    struct Job {
        long id;
        std::string kind, state, name, path, text, error;
    };
    long addJob(const std::string& kind, const std::string& name);
    void finishJob(long id, bool ok, const std::string& path, const std::string& text, const std::string& error);
    void follow(std::string address, std::string session, int tail, unsigned generation);
    void stopFollower();

    std::mutex mu_;
    std::map<std::string, std::string> found_;   // address -> host JSON
    std::atomic<bool> finding_{false};

    std::thread follower_;
    std::atomic<unsigned> generation_{0};
    std::atomic<int> followFd_{-1};
    std::vector<std::string> events_;
    long long base_ = 0;
    bool connected_ = false;
    std::string error_;

    std::vector<Job> jobs_;
    long nextJob_ = 1;
    int recorder_ = -1;
    std::string recording_;
};

}  // namespace agents
