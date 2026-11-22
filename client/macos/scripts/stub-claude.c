// Stand-in for an installed Claude CLI on the CI Mac. The daemon checks the
// foreground executable's identity as well as its terminal's ready prompt.
#include <stdio.h>
#include <string.h>
#include <termios.h>
#include <unistd.h>

int main(int argc, char **argv) {
    if (argc > 1 && (!strcmp(argv[1], "--version") || !strcmp(argv[1], "-v"))) {
        puts("2.1.0 (Claude Code)");
        return 0;
    }
    if (argc > 1 && !strcmp(argv[1], "auth")) {
        puts("{\"loggedIn\":true}");
        return 0;
    }
    struct termios tty;
    if (tcgetattr(STDIN_FILENO, &tty) == -1) return 1;
    struct termios quiet = tty;
    quiet.c_lflag &= ~ECHO;
    if (tcsetattr(STDIN_FILENO, TCSANOW, &quiet) == -1) return 1;
    fputs("\033[?1049h", stdout);
    char line[4096];
    do {
        fputs("\033[H\033[2JClaude Code v2.1.0\n\n❯ \n  ⏵⏵ bypass permissions on (shift+tab to cycle)\n", stdout);
        fflush(stdout);
    } while (fgets(line, sizeof(line), stdin));
    fputs("\033[?1049l", stdout);
    tcsetattr(STDIN_FILENO, TCSANOW, &tty);
    return 0;
}
