import { context, platform } from "nsh";

/**
 * How tight a surface draws (DESIGN-MAC.md; RESULT-DESIGN-PAIRS 11-21): `phone` is the shipped phone and web app,
 * `window` Mewla for Mac's window, `perch` its quick chat. A window's root takes the default, which follows the
 * platform (a context does not cross `Window.open`); the perch provides its own.
 */
export type Density = "phone" | "window" | "perch";

export const DensityContext = context<Density>(platform.os === "macos" ? "window" : "phone");

/** The density of the surface being made; read once, when the component is made. */
export function density(): Density {
  return DensityContext();
}

/** One value per density, the phone's verbatim; the perch takes the window's where it names none. */
export function dense<T>(values: { phone: T; window: T; perch?: T }): T {
  const d = density();
  return d === "phone" ? values.phone : d === "perch" ? (values.perch ?? values.window) : values.window;
}

/**
 * The thread's type (RESULT-DESIGN-PAIRS #1): the phone's 17/25 stays on phones; the Mac's window reads 14.5/23 and
 * its perch 14/21. Read when the row is made.
 */
export function threadType() {
  return {
    /** Brain's text. */
    message: dense({ phone: "text-message", window: "text-[14.5px] leading-[23px]", perch: "text-[14px] leading-[21px]" }),
    /** Your bubble's text, and the bubble around it. */
    bubbleText: dense({ phone: "text-message", window: "text-[14.5px] leading-[21px]", perch: "text-[14px] leading-[20px]" }),
    bubble: dense({ phone: "rounded-(--radius-bubble) px-4 py-3", window: "rounded-[18px] px-[13px] py-2", perch: "rounded-2xl px-3 py-[7px]" }),
    /** The time line under a message. */
    time: dense({ phone: "text-caption", window: "text-[11.5px] leading-[15px]", perch: "text-[11px] leading-[14px]" }),
    /** Sealed and tool-run lines, and a card's source row. */
    line: dense({ phone: "text-sub", window: "text-[12.5px] leading-[17px]", perch: "text-[12px] leading-[16px]" }),
    /** Their monospace (a command). */
    mono: dense({ phone: "text-code", window: "text-[12px] leading-[17px]", perch: "text-[11.5px] leading-[16px]" }),
    /** A card's question. */
    question: dense({ phone: "text-body", window: "text-[14.5px] leading-[21px]", perch: "text-[14px] leading-[20px]" }),
  };
}

/** The composer (RESULT-DESIGN-PAIRS #3): the phone's 52 pill; the window's 42 with a 30 Send, the perch's 38 with 28. */
export function composerSize() {
  return dense({
    phone: {
      dock: "px-3 pt-2",
      field: "min-h-[52px] rounded-[26px] pl-1 pr-1",
      input: "max-h-[128px] min-h-[52px] px-1 py-3.5 text-body",
      hit: "mb-0.5 size-12",
      circle: "size-9",
      arrow: 20,
      plus: { box: "h-[52px] w-11", icon: 22 },
      /** The field's height while the composer is open (a control beside Send). */
      openInput: 64,
    },
    window: {
      dock: "px-3 pt-2",
      field: "min-h-[42px] rounded-[21px] border border-border pl-[5px] pr-[5px]",
      input: "max-h-[128px] min-h-[42px] px-1 py-[11px] text-[14.5px] leading-[20px]",
      hit: "mb-[6px] size-[30px]",
      circle: "size-[30px]",
      arrow: 16,
      plus: { box: "h-[42px] w-[30px]", icon: 18 },
      openInput: 52,
    },
    perch: {
      dock: "px-2.5 pt-2",
      field: "min-h-[38px] rounded-[19px] border border-border pl-[5px] pr-[5px]",
      input: "max-h-[128px] min-h-[38px] px-1 py-[9px] text-[14px] leading-[20px]",
      hit: "mb-[5px] size-7",
      circle: "size-7",
      arrow: 16,
      plus: { box: "h-[38px] w-7", icon: 17 },
      openInput: 48,
    },
  });
}

/** A page's own header on a desktop (RESULT-DESIGN-PAIRS #4): the phone's 52; the Mac window's 46, one line. */
export function headerSize() {
  return dense({
    phone: { bar: "h-[52px] px-4", title: "text-xl font-semibold", back: "size-10", backIcon: 20, mark: 28, name: "text-body font-semibold", sub: "text-sub", oneLine: false, action: 0, actionIcon: 22 },
    window: { bar: "h-[46px] pl-[18px] pr-3", title: "text-[13.5px] leading-[18px] font-semibold", back: "size-7", backIcon: 16, mark: 18, name: "text-[13.5px] leading-[18px] font-semibold", sub: "text-[12px] leading-[16px]", oneLine: true, action: 28, actionIcon: 16 },
  });
}

/**
 * A request card in a thread (Approve, a question, a sign-in): the phone's 48 pt buttons in a 16 pt padded card; the
 * Mac's 30 pt buttons at 13, radius 9, in a 12/14 padded card of radius 16 (DESIGN-MAC sheets, mac.css `.card`, `.btn`).
 */
export function requestCardSize() {
  return dense({
    phone: { card: "gap-3 rounded-(--radius-group) bg-card p-4", button: "h-[48px] rounded-(--radius-control)", label: "text-body", check: 18, option: "min-h-[54px]", field: "min-h-[44px] text-body", later: "-mb-2 -mt-1 h-[44px]", laterText: "text-sub" },
    window: { card: "gap-2.5 rounded-2xl bg-card px-3.5 py-3", button: "h-[30px] rounded-[9px]", label: "text-[13px] leading-[17px]", check: 14, option: "min-h-[40px]", field: "min-h-[32px] text-[13px]", later: "-mb-1 -mt-1 h-[24px]", laterText: "text-[12px]" },
  });
}
