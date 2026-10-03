package xyz.vpavlin.shrooms

import android.content.Context
import android.media.MediaRecorder
import android.os.Build
import java.io.File

/**
 * Records a voice note to send to an agent, which transcribes it on its own
 * machine (shrooms-agent's whisper.cpp): the audio never goes to anybody's
 * speech service. AAC in an MP4 box — small, and anything ffmpeg reads.
 */
class VoiceRecorder(private val ctx: Context) {
    private var rec: MediaRecorder? = null
    private var file: File? = null
    var startedAt = 0L
        private set

    fun start() {
        val f = File(ctx.cacheDir, "voice-${System.currentTimeMillis()}.m4a")
        val r = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) MediaRecorder(ctx)
        else @Suppress("DEPRECATION") MediaRecorder()
        r.setAudioSource(MediaRecorder.AudioSource.MIC)
        r.setOutputFormat(MediaRecorder.OutputFormat.MPEG_4)
        r.setAudioEncoder(MediaRecorder.AudioEncoder.AAC)
        // Speech, for a model that resamples to 16 kHz mono anyway.
        r.setAudioChannels(1)
        r.setAudioSamplingRate(32_000)
        r.setAudioEncodingBitRate(64_000)
        r.setOutputFile(f.absolutePath)
        r.prepare()
        r.start()
        rec = r
        file = f
        startedAt = System.currentTimeMillis()
    }

    /** Stops and returns the recording, or null if there was none worth sending. */
    fun stop(): File? {
        val r = rec ?: return null
        val f = file
        rec = null
        file = null
        // stop() throws when nothing was recorded (a tap straight after start).
        val ok = runCatching { r.stop() }.isSuccess
        r.release()
        return if (ok && f != null && f.length() > 0) f else { f?.delete(); null }
    }

    fun cancel() { stop()?.delete() }

    val recording get() = rec != null
}
