//! The terminal's faces, as on the phones (client/native/terminal/fonts, SIL OFL 1.1): Maple Mono NF Regular (with
//! the Nerd Fonts icons), SemiBold for bold, and the real Italic and SemiBold Italic (Latin, Greek, Cyrillic,
//! punctuation only), carried in the executable and made straight from the bytes (never installed for other apps).
//! Anything they lack (CJK, emoji) falls back to the system's faces through CoreText.

use std::sync::OnceLock;

use objc2_core_foundation::{CFData, CFRetained, CGAffineTransform, CGFloat};
use objc2_core_text::{CTFont, CTFontDescriptor, CTFontManagerCreateFontDescriptorFromData};

static REGULAR: &[u8] = include_bytes!("../../../native/terminal/fonts/MapleMono-NF-Regular.ttf");
static BOLD: &[u8] = include_bytes!("../../../native/terminal/fonts/MapleMono-NF-SemiBold.ttf");
static ITALIC: &[u8] = include_bytes!("../../../native/terminal/fonts/MapleMono-NF-Italic.ttf");
static BOLD_ITALIC: &[u8] = include_bytes!("../../../native/terminal/fonts/MapleMono-NF-SemiBoldItalic.ttf");

/// A descriptor per face, made once (CTFontDescriptor is immutable and thread-safe).
struct Descriptors([Option<SendDescriptor>; 4]);
struct SendDescriptor(CFRetained<CTFontDescriptor>);
// SAFETY: CoreText font descriptors are immutable and documented as thread-safe.
unsafe impl Send for SendDescriptor {}
unsafe impl Sync for SendDescriptor {}

fn descriptors() -> &'static Descriptors {
    static ONCE: OnceLock<Descriptors> = OnceLock::new();
    ONCE.get_or_init(|| {
        let make = |bytes: &'static [u8]| {
            let data = CFData::from_static_bytes(bytes);
            // SAFETY: CoreText reads the data; NULL (None) when it is not a font.
            unsafe { CTFontManagerCreateFontDescriptorFromData(&data) }.map(SendDescriptor)
        };
        Descriptors([make(REGULAR), make(BOLD), make(ITALIC), make(BOLD_ITALIC)])
    })
}

/// The six faces at one size: regular, bold, the real italics, and the upright faces slanted 12° for what the
/// italics lack.
pub struct Faces {
    pub regular: CFRetained<CTFont>,
    pub bold: CFRetained<CTFont>,
    pub italic: CFRetained<CTFont>,
    pub bold_italic: CFRetained<CTFont>,
    pub slant: CFRetained<CTFont>,
    pub bold_slant: CFRetained<CTFont>,
}

fn face(index: usize, size: CGFloat) -> Option<CFRetained<CTFont>> {
    let d = descriptors().0[index].as_ref()?;
    // SAFETY: a valid descriptor; no matrix.
    Some(unsafe { CTFont::with_font_descriptor(&d.0, size, std::ptr::null()) })
}

/// The system's monospaced face, when the bundled ones did not load.
fn system(size: CGFloat, bold: bool) -> CFRetained<CTFont> {
    use objc2_app_kit::{NSFont, NSFontWeightRegular, NSFontWeightSemibold};
    let weight = unsafe { if bold { NSFontWeightSemibold } else { NSFontWeightRegular } };
    let font = NSFont::monospacedSystemFontOfSize_weight(size, weight);
    // SAFETY: NSFont and CTFont are toll-free bridged; the retain moves to the CF side.
    unsafe { CFRetained::retain(std::ptr::NonNull::from(&*font).cast::<CTFont>()) }
}

fn slanted(font: &CTFont) -> CFRetained<CTFont> {
    let skew = CGAffineTransform { a: 1.0, b: 0.0, c: 0.21, d: 1.0, tx: 0.0, ty: 0.0 };
    // SAFETY: a valid matrix for the call.
    unsafe { font.copy_with_attributes(font.size(), &skew, None) }
}

impl Faces {
    pub fn at(size: CGFloat) -> Faces {
        let regular = face(0, size).unwrap_or_else(|| system(size, false));
        let bold = face(1, size).unwrap_or_else(|| system(size, true));
        let slant = slanted(&regular);
        let bold_slant = slanted(&bold);
        let italic = face(2, size).unwrap_or_else(|| slant.clone());
        let bold_italic = face(3, size).unwrap_or_else(|| bold_slant.clone());
        Faces { regular, bold, italic, bold_italic, slant, bold_slant }
    }

    /// Face `index`: 0 regular, 1 bold, 2 italic, 3 bold italic.
    pub fn get(&self, index: usize) -> &CTFont {
        match index {
            1 => &self.bold,
            2 => &self.italic,
            3 => &self.bold_italic,
            _ => &self.regular,
        }
    }
}
