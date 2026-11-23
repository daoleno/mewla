// The bundle's executable in the probe: posix_spawns Contents/Helpers/probe with its own arguments and waits, as a
// host that keeps the daemon as its child would. Responsibility (which process TCC charges) is not disclaimed.
#include <libgen.h>
#include <mach-o/dyld.h>
#include <spawn.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/wait.h>

extern char **environ;

int main(int argc, char **argv) {
  char exe[4096];
  uint32_t size = sizeof exe;
  if (_NSGetExecutablePath(exe, &size) != 0) return 2;
  char helper[4096];
  snprintf(helper, sizeof helper, "%s/../Helpers/probe", dirname(exe));
  argv[0] = helper;
  pid_t pid;
  if (posix_spawn(&pid, helper, NULL, NULL, argv, environ) != 0) return 3;
  int status = 0;
  waitpid(pid, &status, 0);
  return WIFEXITED(status) ? WEXITSTATUS(status) : 1;
}
