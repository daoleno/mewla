package expo.modules.mewlaphone.helper

/** An Android user on the phone: the person's own (0), or the one an app clone (应用分身) or a work profile runs as. */
internal data class PhoneUser(val id: Int, val name: String, val running: Boolean)

/** What the shell says about the phone's other users, parsed. Clones run as another user, usually 999 on ColorOS. */
internal object Clones {
    private val userLine = Regex("""UserInfo\{(\d+):([^:}]*):[0-9a-fA-F]+\}(.*)""")
    private val component = Regex("""^([A-Za-z0-9_.]+)/(\S+)$""")

    /** The users in `pm list users`, the person's own (0) left out. */
    fun parseUsers(output: String): List<PhoneUser> = output.lineSequence().mapNotNull { line ->
        val m = userLine.find(line) ?: return@mapNotNull null
        val id = m.groupValues[1].toInt()
        if (id == 0) null else PhoneUser(id, m.groupValues[2], m.groupValues[3].contains("running"))
    }.toList()

    /** The packages that have a launcher activity in the output of `cmd package query-activities --components`. */
    fun parsePackages(output: String): List<String> = output.lineSequence()
        .mapNotNull { component.find(it.trim())?.groupValues?.get(1) }
        .distinct().toList()

    /**
     * The launcher component of each package in that output, as the system wrote it (`com.tencent.mm/.ui.LauncherUI`),
     * which `am start -n` takes as it is: the first one a package lists.
     */
    fun parseComponents(output: String): Map<String, String> {
        val found = LinkedHashMap<String, String>()
        for (line in output.lineSequence()) {
            val m = component.find(line.trim()) ?: continue
            found.putIfAbsent(m.groupValues[1], line.trim())
        }
        return found
    }

    /** How a clone is told from the app it copies; the user id makes two clones different. */
    fun label(base: String, userId: Int, chinese: Boolean): String = if (chinese) "$base (分身 $userId)" else "$base (clone $userId)"

    /** Whether `am start` refused because the user is not running, so starting it once may help. */
    fun needsStart(output: String): Boolean =
        output.contains("is stopped", ignoreCase = true) || output.contains("not running", ignoreCase = true) ||
            output.contains("User is not started", ignoreCase = true)

    /** Whether the output of `am start` is a refusal. */
    fun failed(output: String): Boolean = output.contains("Error", ignoreCase = true)

    /** What both starts said, raw, in one line for the agent's error. */
    fun refusal(amStart: String, startActivity: String): String =
        "am start: ${amStart.trim().take(200)}; cmd activity start-activity: ${startActivity.trim().take(200)}"
}
