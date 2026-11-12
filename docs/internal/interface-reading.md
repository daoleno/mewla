# Interface Reading

Android and iOS share one reading policy. At the newest edge, appended and
streaming content remains followed. A manual move into history detaches that
intent. Data, layout, keyboard and connection events cannot attach it again;
an explicit return to latest or intentional send can establish a new intent.
Subsequent manual scrolling cancels pending send-focus effects.

## Position Ownership

`Timeline` (`client/ui/chat/Timeline.tsx`) owns the reading position of every
chat. A chat opens at its newest row (`initialScrollAtEnd`).
`maintainVisibleContentPosition` with `autoscrollToBottomThreshold: 80` keeps a
reader at the newest edge following appended and streaming rows, and holds the
rows steady for a reader who has scrolled up. More than 240 pt from the end, a
Jump to latest button appears, labelled with the age of the newest item; it
scrolls to the end. A person's own send brings their newest message to the top
(`sent`), with the reply under it.

The place a chat was read to is the key of its first visible row
(`readingPlace` in `client/core/timeline/readingPlace.ts`), not a pixel offset: a
reopened list measures its rows as they show, and an offset saved from a
measured list can land past what the new one has measured. The place is kept per
chat scope in a process-local map, written only after the person's own drag
(`onScrollDragStart`), so the list settling as it mounts never moves it, and
cleared when the person is back near the end. Reopening finds the row again by
index (`readingPlaceIndex`); if it is gone, the chat opens at its newest row. No
transcript copy is stored, and cold process death is not a persistent
reading-position guarantee.

On the web, a hidden tab's list loses its scroll (`display: none`). A chat given
`shown` records its offset while on screen and, when it is shown again, scrolls
back to that offset or to the newest row.

## Image Preview

File references open `client/ui/more/files/FilePreview.tsx` over the Session's
read capability (`client/core/files/sessionFilePreview.ts`). Changing server,
daemon, Session, process or start closes the preview, and a missing or changed
file stays retryable without keeping the old content. A video or audio file plays
through `MediaPlayer` (`Media.tsx`), which signs a fresh capability when the
2-minute one lapses. An unavailable image remains an explicit file error rather
than a blank success.

## Verification

`client/core/timeline/readingPlace.test.ts` covers the saved place;
`client/core/files/sessionFilePreview.test.ts` covers the preview state, including
closing on a context change and retrying a changed file.
`client/ui/chat/BrainThread.test.tsx` and the other `nsh test` files under
`client/ui` run the screens.

Run `cd client && bun test core` and `cd client/ui && nsh check && nsh test`.
Native PID/log and before/after screen evidence remain necessary for crash,
first-image, scrolling and keyboard claims. A passing test run is not a signed
client release or iOS device proof.

## Shared images and attachment selection

Message attachments, tool images, Markdown images and composer previews use the
typed image sources in `client/core/files/imageSource.ts` on every platform.
Persisted provider paths resolve through the Session's file capability.
External HTTP images receive no Mewla authorization. A data URL image must be
PNG, JPEG, GIF, WebP or SVG and at most 8 MiB encoded. SVG goes through
`client/core/files/svgPreview.ts`, which reads at most 2 MiB and checks the
structure before it is drawn. The daemon refuses images above 40 megapixels in
its previews (`daemon/attachment/image_preview.go`). Text and code remain
selectable.

The attachment chooser (`files.pick {maxCount}`, answered by the Android or iOS
host, or the browser) selects multiple files in one confirmation. The composer
reserves the remaining slots within the eight-file limit
(`MAX_COMPOSER_ATTACHMENTS`) before dispatch, and an error is shown when the
picker returns more. `AttachmentTray` (`client/ui/more/files/Attachments.tsx`)
shows one chip per picked file in order, with progress while the upload is
pending, Cancel, Retry on failure and Remove; a picture shows itself and a tap
opens it whole, zoomable by pinch or double tap. Sending waits until attachments
are ready or removed.

Attachments are typed fields end to end. The phone sends `send_input` with
`text` (what the person wrote) and `attachments: [{name, path, content_type}]`
as separate fields, queues them together in the send outbox, and the daemon is
the only place that renders them into provider input (`attachment.Message.Render`
in `daemon/attachment/input.go`: the text, then a `<mewla_attachments>` JSON tag
the Worker reads to learn the uploaded paths; golden-tested, one function for every
provider and sender, including Telegram). Providers and the DSH terminal only
accept text, so they echo that rendering back in their transcripts; the daemon
reads it once at the projection boundary (`attachment.Split`, called from
`work.ProjectUserAttachments`) and gives the phone `attachments` on the
`user_message` event with `body` as the text alone. The app never reads or writes
the tag, and `content_type` is kept so a supported image without a file extension
remains an image when history reloads. The Brain admission digest covers the exact
rendered provider input, so the files are part of what it matches the provider's
echo against. Rows stored before attachments were typed are not special-cased in
the app; the retired `Uploaded files:` text list is plain text.
