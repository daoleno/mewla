//! The terminal view's native half on macOS (src/terminal): the packer and glyphs both phones share
//! (client/native/terminal/TerminalVt.cpp, TerminalGlyphs.cpp) over the static libghostty-vt the darwin daemon links
//! (scripts/build-libghostty-desktop.sh: `<dir>/include/ghostty/` and `<dir>/darwin_arm64/libghostty-vt.a`).
//!
//! Where that library is: `MEWLA_GHOSTTY_DESKTOP_CACHE`, else the macOS workflows' cache
//! (`~/.cache/mewla/ghostty-release/darwin_arm64`), else build-daemon-linux.sh's default
//! (`~/.cache/mewla/ghostty-desktop`), else `daemon/ghostty-libs`. Other targets (the Linux `cargo test` of the
//! host's portable parts) build none of it.

use std::path::{Path, PathBuf};

fn main() {
    println!("cargo:rerun-if-env-changed=MEWLA_GHOSTTY_DESKTOP_CACHE");
    println!("cargo:rerun-if-changed=build.rs");
    if std::env::var("CARGO_CFG_TARGET_OS").as_deref() != Ok("macos") {
        return;
    }
    let manifest = PathBuf::from(std::env::var("CARGO_MANIFEST_DIR").expect("cargo sets CARGO_MANIFEST_DIR"));
    let root = manifest.join("../..");
    let native = root.join("client/native/terminal");
    let home = std::env::var_os("HOME").map(PathBuf::from).unwrap_or_default();
    let xdg = std::env::var_os("XDG_CACHE_HOME").map(PathBuf::from).unwrap_or_else(|| home.join(".cache"));
    let mut dirs: Vec<PathBuf> = Vec::new();
    if let Some(dir) = std::env::var_os("MEWLA_GHOSTTY_DESKTOP_CACHE") {
        dirs.push(PathBuf::from(dir));
    }
    dirs.push(home.join(".cache/mewla/ghostty-release/darwin_arm64"));
    dirs.push(xdg.join("mewla/ghostty-desktop"));
    dirs.push(root.join("daemon/ghostty-libs"));
    let found = dirs.iter().find(|dir| dir.join("darwin_arm64/libghostty-vt.a").is_file());

    // The headers that go with the library; when only checking (no darwin library here), any cache's, or the
    // pinned ones scripts/build-libghostty.sh unpacks into native/terminal-vt.
    let has_headers = |dir: &PathBuf| dir.join("include/ghostty/vt.h").is_file();
    let include = found
        .filter(|dir| has_headers(dir))
        .or_else(|| dirs.iter().find(|dir| has_headers(dir)))
        .map(|dir| dir.join("include"))
        .unwrap_or_else(|| root.join("native/terminal-vt/include"));
    for file in ["TerminalVt.cpp", "TerminalGlyphs.cpp", "TerminalVt.h", "TerminalGlyphs.h"] {
        println!("cargo:rerun-if-changed={}", native.join(file).display());
    }
    cc::Build::new()
        .cpp(true)
        .std("c++17")
        .file(native.join("TerminalVt.cpp"))
        .file(native.join("TerminalGlyphs.cpp"))
        .include(&include)
        .include(&native)
        .warnings(false)
        .compile("mewla_terminal_native");

    match found {
        Some(dir) => {
            link(dir);
        }
        None => println!(
            "cargo:warning=no darwin_arm64/libghostty-vt.a in {}: the link will fail (scripts/build-libghostty-desktop.sh --targets darwin_arm64)",
            dirs.iter().map(|d| d.display().to_string()).collect::<Vec<_>>().join(", ")
        ),
    }
}

fn link(dir: &Path) {
    let lib = dir.join("darwin_arm64");
    println!("cargo:rerun-if-changed={}", lib.join("libghostty-vt.a").display());
    println!("cargo:rustc-link-search=native={}", lib.display());
    println!("cargo:rustc-link-lib=static=ghostty-vt");
}
