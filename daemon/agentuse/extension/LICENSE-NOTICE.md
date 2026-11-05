# Notice

This extension's relay (relay.js) and tab-group handling (group.js) are derived
from the Playwright Extension (microsoft/playwright, packages/extension),
Copyright (c) Microsoft Corporation, licensed under the Apache License,
Version 2.0 (https://www.apache.org/licenses/LICENSE-2.0). They are changed:
the connect page, the shared key and tab picking are gone; the extension is
asked to connect by Mewla's native messaging host, and works in a window of its
own.
