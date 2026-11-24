//! Whether the text under the pointer is a link, for the pointer's shape only: what a click on it opens is the page's
//! (core/terminal/text.ts `linkAction`, from `tapCell`). Plain strings, so `cargo test` runs it anywhere.

/// Characters that end a link on either side.
fn stops(c: char) -> bool {
    c.is_whitespace() || matches!(c, '"' | '\'' | '<' | '>' | '`' | '(' | ')' | '[' | ']' | '{' | '}' | '|')
}

/// Whether UTF-16 index `index` of `text` (a row's text, `tapCell`'s `index`) is inside a URL.
pub fn url_at(text: &str, index: isize) -> bool {
    if index < 0 {
        return false;
    }
    let chars: Vec<(usize, char)> = {
        let mut at = 0usize;
        text.chars()
            .map(|c| {
                let here = at;
                at += c.len_utf16();
                (here, c)
            })
            .collect()
    };
    let Some(k) = chars.iter().position(|(at, c)| *at <= index as usize && (index as usize) < at + c.len_utf16()) else {
        return false;
    };
    if stops(chars[k].1) {
        return false;
    }
    let mut a = k;
    while a > 0 && !stops(chars[a - 1].1) {
        a -= 1;
    }
    let mut b = k;
    while b + 1 < chars.len() && !stops(chars[b + 1].1) {
        b += 1;
    }
    let word: String = chars[a..=b].iter().map(|(_, c)| *c).collect();
    let lower = word.to_ascii_lowercase();
    ["https://", "http://", "file://"].iter().any(|scheme| lower.find(scheme).is_some_and(|at| lower.len() > at + scheme.len()))
}

#[cfg(test)]
mod tests {
    use super::url_at;

    #[test]
    fn urls_under_the_pointer() {
        let row = "see https://mewla.dev/docs (or not)";
        assert!(url_at(row, 4));
        assert!(url_at(row, 20));
        assert!(!url_at(row, 2));
        assert!(!url_at(row, 27));
        assert!(!url_at(row, -1));
        assert!(!url_at("http://", 2));
        assert!(url_at("界 http://a.b", 4));
        assert!(!url_at("plain/path/file.rs", 3));
    }
}
