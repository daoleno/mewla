//! `mewla.terminal` (client/ui/terminal/contract.ts) on the Mac: the terminal as an AppKit view in NSH's native
//! component slot, as client/ios's TerminalComponent is on iOS.

pub mod keys;
pub mod links;
pub mod vt;
#[cfg(target_os = "macos")]
mod fonts;
#[cfg(target_os = "macos")]
mod view;

#[cfg(target_os = "macos")]
pub use component::register;

#[cfg(target_os = "macos")]
mod component {
    use std::rc::Rc;

    use nsh_platform_macos::{ComponentContext, Components, NativeComponent};
    use objc2::MainThreadMarker;
    use objc2::rc::Retained;
    use objc2_app_kit::NSView;
    use serde_json::Value;

    use super::view::TermView;

    struct Terminal {
        view: Retained<TermView>,
    }

    fn num(args: &Value, key: &str) -> Option<i64> {
        args.get(key).and_then(Value::as_i64)
    }

    impl NativeComponent for Terminal {
        fn view(&self) -> Retained<NSView> {
            Retained::into_super(self.view.clone())
        }

        fn set_prop(&mut self, name: &str, value: Option<&Value>) {
            match name {
                "fontSize" => {
                    if let Some(size) = value.and_then(Value::as_f64) {
                        self.view.set_font_size(size);
                    }
                }
                "theme" => {
                    let Some(theme) = value else { return };
                    let text = |key: &str| theme.get(key).and_then(Value::as_str).unwrap_or("");
                    let palette: Vec<String> = theme
                        .get("palette")
                        .and_then(Value::as_array)
                        .map(|p| p.iter().filter_map(|c| c.as_str().map(str::to_string)).collect())
                        .unwrap_or_default();
                    self.view.set_theme(text("background"), text("foreground"), text("cursor"), text("selection"), &palette);
                }
                _ => {}
            }
            self.view.flush();
        }

        fn command(&mut self, name: &str, args: &Value) {
            let v = &self.view;
            match name {
                "write" => {
                    if let Some(data) = args.get("data").and_then(Value::as_str) {
                        v.write(data);
                    }
                }
                "reset" => v.reset(),
                "history" => v.apply_history(
                    args.get("reset").and_then(Value::as_bool).unwrap_or(false),
                    num(args, "removed").unwrap_or(0).max(0) as usize,
                    args.get("rows")
                        .and_then(Value::as_array)
                        .map(|rows| rows.iter().map(|r| r.as_str().unwrap_or("").to_string()).collect())
                        .unwrap_or_default(),
                    num(args, "cols").unwrap_or(0).max(0) as usize,
                    args.get("alternate").and_then(Value::as_bool).unwrap_or(false),
                ),
                "scrollTo" => {
                    if let Some(row) = num(args, "row") {
                        v.scroll_to(row.max(0) as usize);
                    }
                }
                "scrollToBottom" => v.scroll_to_bottom(),
                "mark" => v.mark(num(args, "row").unwrap_or(-1) as isize),
                "focus" => v.focus(),
                "blur" => v.blur(),
                "retry" => v.retry(),
                "paste" => {
                    if let Some(text) = args.get("text").and_then(Value::as_str) {
                        v.paste(text);
                    }
                }
                // `sessionSize` (another viewer's size, drawn fitted) comes with a later slice.
                _ => {}
            }
            v.flush();
        }
    }

    /// Registers `mewla.terminal` (main.rs, before `run_app`).
    pub fn register(components: &Components) {
        components.register(
            "mewla.terminal",
            Rc::new(|ctx: ComponentContext| -> Box<dyn NativeComponent> {
                let mtm = MainThreadMarker::new().expect("native components are made on the main thread");
                Box::new(Terminal { view: TermView::new(mtm, ctx) })
            }),
        );
    }
}
