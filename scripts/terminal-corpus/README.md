# Terminal render corpus (`run.sh`)
Open fault, 11-21: on CI only, Firefox hangs in `page.goto` (30 s timeout) on about the 17th–19th screen, in 2 of 3 runs, on a different screen each time; it never hangs on a dev machine or in `ubuntu:24.04` (also CPU-limited), and Chromium and WebKit never do.
`domcontentloaded` instead of `load` did not help; a whole-browser retry (`run.sh`) sometimes does, and a fresh browser per screen with a watchdog hung again after relaunch (reverted); the cause is unknown.
So Firefox is advisory in CI (`CORPUS_ADVISORY=firefox`: its result and logs are printed as a warning, the job stays green); locally it is a real pass or fail.
