// Cat mascot pose library: each pose returns an SVG string.
// Palette: body #f0a35e, shade #e5872f, cream #fbdcb4, ink #4a3328, blush #f4a0a0, nose #d9776a
const P = { body: '#f0a35e', shade: '#e5872f', cream: '#fbdcb4', ink: '#4a3328', blush: '#f0a0a0', nose: '#d9776a' };
const dot = (c) => `<circle r="${c.r}" cx="${c.x}" cy="${c.y}" fill="${c.f || P.body}"/>`;

// closed eye: downward arc
const eye = (x, y, w = 7, sw = 2.4, ink = P.ink) =>
  `<path d="M${x - w / 2} ${y} Q${x} ${y + w * 0.62} ${x + w / 2} ${y}" fill="none" stroke="${ink}" stroke-width="${sw}" stroke-linecap="round"/>`;
const blush = (x, y) => `<ellipse cx="${x}" cy="${y}" rx="5.2" ry="3.1" fill="${P.blush}" opacity=".45"/>`;
const noseMouth = (x, y) =>
  `<path d="M${x - 2.1} ${y} h4.2 l-2.1 2.5 z" fill="${P.nose}" stroke="${P.nose}" stroke-width="1.4" stroke-linejoin="round"/>` +
  `<path d="M${x} ${y + 2.3} q0 3 -3 3.4 M${x} ${y + 2.3} q0 3 3 3.4" fill="none" stroke="${P.ink}" stroke-width="1.5" stroke-linecap="round"/>`;

// ---- poses ----------------------------------------------------------------
// All poses sit in a 200x140 viewBox, ground line ~ y=122.

// Curled up asleep, seen from the side.
const sleep = () => `
  <ellipse cx="102" cy="124" rx="72" ry="5" fill="#000" opacity=".05"/>
  <path d="M62 118 C 56 78, 84 44, 122 44 C 158 44, 182 74, 182 104 C 182 116, 176 122, 164 122 Z" fill="${P.body}"/>
  <g fill="none" stroke="${P.shade}" stroke-width="6" stroke-linecap="round">
    <path d="M112 48 Q 118 60 112 72"/><path d="M136 46 Q 143 60 138 74"/><path d="M158 54 Q 164 66 161 79"/>
  </g>
  <path d="M28 74 L 24 40 Q 24 35 28.5 38 L 53 58 Z" fill="${P.body}"/>
  <path d="M60 58 L 84 38 Q 88.5 35 88.5 40 L 85 74 Z" fill="${P.body}"/>
  <path d="M31 66 L 29 47 L 45 58 Z" fill="${P.cream}"/>
  <path d="M70 58 L 83 47 L 82 66 Z" fill="${P.cream}"/>
  <rect x="52" y="108" width="26" height="15" rx="7.5" fill="${P.cream}"/>
  <rect x="76" y="108" width="26" height="15" rx="7.5" fill="${P.cream}"/>
  <path d="M14 86 C 14 62, 32 46, 58 46 C 84 46, 102 62, 102 86 C 102 106, 84 118, 58 118 C 32 118, 14 106, 14 86 Z" fill="${P.body}"/>
  <path d="M46 48 Q 58 57 70 48" fill="none" stroke="${P.shade}" stroke-width="5" stroke-linecap="round"/>
  ${eye(36, 84, 9, 2.8)}${eye(66, 84, 9, 2.8)}
  ${blush(28, 96)}${blush(86, 96)}
  ${noseMouth(51, 92)}
  <path d="M176 110 C 170 128, 130 130, 104 124" fill="none" stroke="${P.body}" stroke-width="15" stroke-linecap="round"/>
  <path d="M104 124 C 100 123, 98 122, 96 120" fill="none" stroke="${P.shade}" stroke-width="15" stroke-linecap="round"/>`;

// Loaf: lying flat, head upright, paws tucked, breathing.
const loaf = () => `
  <ellipse cx="100" cy="124" rx="66" ry="5" fill="#000" opacity=".05"/>
  <path d="M40 122 C 34 96, 58 76, 100 74 C 146 72, 170 92, 170 110 C 170 119, 164 122, 154 122 Z" fill="${P.body}"/>
  <g fill="none" stroke="${P.shade}" stroke-width="5.5" stroke-linecap="round">
    <path d="M120 78 Q 126 88 121 98"/><path d="M142 82 Q 148 92 144 102"/>
  </g>
  <path d="M40 78 L 36 40 Q 36 34 41 38 L 66 58 Z" fill="${P.body}"/>
  <path d="M92 58 L 116 38 Q 121 34 121 40 L 117 78 Z" fill="${P.body}"/>
  <path d="M44 70 L 42 47 L 58 58 Z" fill="${P.cream}"/>
  <path d="M104 58 L 116 48 L 115 70 Z" fill="${P.cream}"/>
  <path d="M28 92 C 28 68, 48 52, 78 52 C 108 52, 128 68, 128 92 C 128 112, 108 124, 78 124 C 48 124, 28 112, 28 92 Z" fill="${P.body}"/>
  <path d="M64 54 Q 78 64 92 54" fill="none" stroke="${P.shade}" stroke-width="5" stroke-linecap="round"/>
  ${eye(54, 90, 9, 2.8)}${eye(88, 90, 9, 2.8)}
  ${blush(44, 103)}${blush(112, 103)}
  ${noseMouth(71, 99)}
  <rect x="66" y="112" width="24" height="13" rx="6.5" fill="${P.cream}"/>
  <path d="M166 116 C 160 128, 140 126, 130 122" fill="none" stroke="${P.body}" stroke-width="13" stroke-linecap="round"/>`;

// Sitting up, tail curled around the front paws.
const sit = () => `
  <ellipse cx="100" cy="124" rx="52" ry="5" fill="#000" opacity=".05"/>
  <path d="M62 124 C 54 100, 62 74, 78 62 L 122 62 C 138 74, 146 100, 138 124 Z" fill="${P.body}"/>
  <path d="M78 62 C 78 44, 90 30, 100 30 C 110 30, 122 44, 122 62 Z" fill="${P.body}"/>
  <path d="M74 52 L 70 14 Q 70 8 75 12 L 96 34 Z" fill="${P.body}"/>
  <path d="M126 52 L 130 14 Q 130 8 125 12 L 104 34 Z" fill="${P.body}"/>
  <path d="M78 44 L 76 21 L 90 34 Z" fill="${P.cream}"/>
  <path d="M122 44 L 124 21 L 110 34 Z" fill="${P.cream}"/>
  <path d="M72 62 C 72 38, 84 22, 100 22 C 116 22, 128 38, 128 62 C 128 82, 116 94, 100 94 C 84 94, 72 82, 72 62 Z" fill="${P.body}"/>
  <path d="M88 24 Q 100 34 112 24" fill="none" stroke="${P.shade}" stroke-width="5" stroke-linecap="round"/>
  ${eye(90, 60, 9, 2.8)}${eye(112, 60, 9, 2.8)}
  ${blush(80, 74)}${blush(122, 74)}
  ${noseMouth(100, 70)}
  <ellipse cx="90" cy="112" rx="13" ry="9" fill="${P.cream}"/>
  <ellipse cx="110" cy="112" rx="13" ry="9" fill="${P.cream}"/>
  <path d="M60 122 C 62 108, 50 104, 46 96 C 42 88, 50 82, 58 88 C 68 96, 74 108, 74 122 Z" fill="${P.body}" opacity=".001"/>
  <path d="M146 122 C 148 104, 136 96, 132 86" fill="none" stroke="${P.body}" stroke-width="15" stroke-linecap="round"/>
  <path d="M132 86 C 130 82, 130 78, 132 74" fill="none" stroke="${P.shade}" stroke-width="15" stroke-linecap="round"/>`;

// Play bow: front down, rear up, tail high.
const stretch = () => `
  <ellipse cx="104" cy="124" rx="66" ry="5" fill="#000" opacity=".05"/>
  <path d="M74 122 C 66 96, 108 74, 148 84 C 168 90, 172 112, 166 122 Z" fill="${P.body}"/>
  <path d="M138 92 C 152 70, 168 66, 172 56" fill="none" stroke="${P.body}" stroke-width="15" stroke-linecap="round"/>
  <path d="M172 56 C 176 50, 176 44, 172 40" fill="none" stroke="${P.shade}" stroke-width="15" stroke-linecap="round"/>
  <rect x="96" y="104" width="14" height="22" rx="6" fill="${P.body}"/>
  <rect x="140" y="102" width="14" height="24" rx="6" fill="${P.body}"/>
  <path d="M46 66 L 34 30 Q 32 24 38 27 L 64 48 Z" fill="${P.body}"/>
  <path d="M92 46 L 108 22 Q 112 16 113 23 L 114 62 Z" fill="${P.body}"/>
  <path d="M48 58 L 42 38 L 58 51 Z" fill="${P.cream}"/>
  <path d="M98 48 L 107 34 L 107 56 Z" fill="${P.cream}"/>
  <path d="M34 84 C 34 58, 52 42, 80 42 C 106 42, 124 58, 124 84 C 124 106, 106 118, 80 118 C 52 118, 34 106, 34 84 Z" fill="${P.body}"/>
  <path d="M66 44 Q 80 54 94 44" fill="none" stroke="${P.shade}" stroke-width="5" stroke-linecap="round"/>
  <path d="M60 84 Q 66 90 72 84 M88 84 Q 94 90 100 84" fill="none" stroke="${P.ink}" stroke-width="3" stroke-linecap="round"/>
  ${blush(54, 96)}${blush(108, 96)}
  ${noseMouth(80, 92)}
  <rect x="44" y="106" width="40" height="14" rx="7" fill="${P.cream}"/>`;

// Walking, four legs alternating.
const walk = () => `
  <ellipse cx="104" cy="126" rx="64" ry="4.5" fill="#000" opacity=".05"/>
  <path d="M48 112 C 40 88, 66 68, 106 68 C 146 68, 168 86, 168 106 C 168 114, 162 118, 152 118 Z" fill="${P.body}"/>
  <path d="M104 74 Q 110 86 105 98 M132 76 Q 138 88 134 100" fill="none" stroke="${P.shade}" stroke-width="5.5" stroke-linecap="round"/>
  <g class="legs">
    <rect x="66" y="110" width="12" height="16" rx="6" fill="${P.body}"/>
    <rect x="94" y="112" width="12" height="14" rx="6" fill="${P.body}" opacity=".85"/>
    <rect x="132" y="110" width="12" height="16" rx="6" fill="${P.body}"/>
    <rect x="152" y="112" width="12" height="14" rx="6" fill="${P.body}" opacity=".85"/>
  </g>
  <path d="M148 84 C 162 68, 178 66, 184 56" fill="none" stroke="${P.body}" stroke-width="13" stroke-linecap="round"/>
  <path d="M40 66 L 28 32 Q 26 26 32 29 L 58 48 Z" fill="${P.body}"/>
  <path d="M86 46 L 102 24 Q 106 18 107 25 L 108 62 Z" fill="${P.body}"/>
  <path d="M42 58 L 36 40 L 52 51 Z" fill="${P.cream}"/>
  <path d="M92 48 L 101 36 L 101 56 Z" fill="${P.cream}"/>
  <path d="M30 84 C 30 58, 46 42, 74 42 C 100 42, 118 58, 118 84 C 118 106, 100 118, 74 118 C 46 118, 30 106, 30 84 Z" fill="${P.body}"/>
  <path d="M60 44 Q 74 54 88 44" fill="none" stroke="${P.shade}" stroke-width="5" stroke-linecap="round"/>
  <path d="M56 84 Q 62 90 68 84 M84 84 Q 90 90 96 84" fill="none" stroke="${P.ink}" stroke-width="3" stroke-linecap="round"/>
  ${blush(50, 96)}${blush(102, 96)}
  ${noseMouth(76, 92)}`;

// Kneading a cushion.
const knead = () => `
  <ellipse cx="100" cy="126" rx="60" ry="4.5" fill="#000" opacity=".05"/>
  <path d="M40 122 C 32 108, 40 96, 56 96 L 144 96 C 160 96, 168 108, 160 122 Z" fill="#c9b79a"/>
  <path d="M62 122 C 54 96, 66 66, 88 56 L 116 56 C 138 66, 150 96, 142 122 Z" fill="${P.body}"/>
  <path d="M74 50 L 70 14 Q 70 8 75 12 L 94 32 Z" fill="${P.body}"/>
  <path d="M126 50 L 130 14 Q 130 8 125 12 L 106 32 Z" fill="${P.body}"/>
  <path d="M78 42 L 76 21 L 88 32 Z" fill="${P.cream}"/>
  <path d="M122 42 L 124 21 L 112 32 Z" fill="${P.cream}"/>
  <path d="M70 62 C 70 38, 82 22, 100 22 C 118 22, 130 38, 130 62 C 130 82, 118 94, 100 94 C 82 94, 70 82, 70 62 Z" fill="${P.body}"/>
  <path d="M88 24 Q 100 34 112 24" fill="none" stroke="${P.shade}" stroke-width="5" stroke-linecap="round"/>
  ${eye(89, 60, 9, 2.8)}${eye(111, 60, 9, 2.8)}
  ${blush(78, 74)}${blush(122, 74)}
  ${noseMouth(100, 70)}
  <g class="paws">
    <ellipse cx="84" cy="102" rx="12" ry="8" fill="${P.cream}"/>
    <ellipse cx="116" cy="102" rx="12" ry="8" fill="${P.cream}"/>
  </g>`;

// Peeking over an edge: ears, eyes and two paws above a line.
const peek = () => `
  <path d="M0 104 L 200 104 L 200 140 L 0 140 Z" fill="#c9b79a"/>
  <path d="M70 104 L 64 60 Q 64 52 70 57 L 96 82 Z" fill="${P.body}"/>
  <path d="M130 104 L 136 60 Q 136 52 130 57 L 104 82 Z" fill="${P.body}"/>
  <path d="M76 96 L 72 71 L 88 84 Z" fill="${P.cream}"/>
  <path d="M124 96 L 128 71 L 112 84 Z" fill="${P.cream}"/>
  <path d="M62 104 C 62 74, 78 56, 100 56 C 122 56, 138 74, 138 104 Z" fill="${P.body}"/>
  <path d="M86 58 Q 100 70 114 58" fill="none" stroke="${P.shade}" stroke-width="5" stroke-linecap="round"/>
  ${eye(86, 88, 10, 3)}${eye(114, 88, 10, 3)}
  ${blush(74, 98)}${blush(126, 98)}
  ${noseMouth(100, 92)}
  <ellipse cx="72" cy="106" rx="13" ry="8" fill="${P.cream}"/>
  <ellipse cx="128" cy="106" rx="13" ry="8" fill="${P.cream}"/>
  <g stroke="${P.shade}" stroke-width="2" stroke-linecap="round">
    <path d="M68 104 v3 M76 104 v3 M124 104 v3 M132 104 v3"/>
  </g>`;

// Pouncing after a ball of yarn.
const pounce = () => `
  <ellipse cx="124" cy="126" rx="34" ry="5" fill="#000" opacity=".05"/>
  <path d="M118 108 C 96 108, 84 96, 78 82 C 74 72, 80 62, 92 60 C 106 58, 120 66, 126 80 C 132 94, 130 106, 118 108 Z" fill="${P.body}" opacity=".001"/>
  <path d="M52 92 C 40 76, 56 52, 84 44 C 112 36, 140 48, 148 68 C 154 84, 146 100, 128 104 C 106 108, 68 106, 52 92 Z" fill="${P.body}"/>
  <path d="M148 68 C 164 62, 176 66, 184 76" fill="none" stroke="${P.body}" stroke-width="13" stroke-linecap="round"/>
  <path d="M96 52 Q 102 62 98 72 M120 50 Q 126 60 122 70" fill="none" stroke="${P.shade}" stroke-width="5.5" stroke-linecap="round"/>
  <path d="M56 96 L 44 60 Q 42 54 48 57 L 74 78 Z" fill="${P.body}"/>
  <path d="M110 78 L 130 60 Q 135 56 135 62 L 128 96 Z" fill="${P.body}"/>
  <path d="M58 88 L 52 68 L 68 80 Z" fill="${P.cream}"/>
  <path d="M118 80 L 129 69 L 127 88 Z" fill="${P.cream}"/>
  <path d="M42 78 C 42 54, 58 38, 84 38 C 108 38, 124 54, 124 78 C 124 98, 108 110, 84 110 C 58 110, 42 98, 42 78 Z" fill="${P.body}"/>
  <path d="M72 40 Q 84 50 96 40" fill="none" stroke="${P.shade}" stroke-width="5" stroke-linecap="round"/>
  <path d="M68 78 Q 74 84 80 78 M92 78 Q 98 84 104 78" fill="none" stroke="${P.ink}" stroke-width="3" stroke-linecap="round"/>
  ${blush(62, 90)}${blush(112, 90)}
  ${noseMouth(86, 86)}
  <g class="yarn">
    <circle cx="168" cy="116" r="14" fill="#f07d7d"/>
    <path d="M158 110 Q 168 118 178 110 M158 122 Q 168 114 178 122" fill="none" stroke="#d96565" stroke-width="2" stroke-linecap="round"/>
    <path d="M154 118 Q 130 124 118 112" fill="none" stroke="#f07d7d" stroke-width="2.4" stroke-linecap="round"/>
  </g>`;

export const POSES = { sleep, loaf, sit, stretch, walk, knead, peek, pounce };

// Browser helpers -----------------------------------------------------------
export function catSVG(name, { cls = '', aria = '' } = {}) {
  const fn = POSES[name] || POSES.sleep;
  return `<svg viewBox="0 0 200 140" class="cat cat-${name} ${cls}" ${aria ? `role="img" aria-label="${aria}"` : 'aria-hidden="true"'}>${fn()}</svg>`;
}

// Drifting sleep marks, positioned next to a cat.
export function zzz({ n = 3, x = 62, y = 30 } = {}) {
  let out = '';
  for (let i = 0; i < n; i++) {
    out += `<span class="z" style="--i:${i};left:${x + i * 12}%;top:${y - i * 4}%">z</span>`;
  }
  return out;
}

// Spread across a section: a parade of poses.
export function parade(list, opts = {}) {
  return list.map((n, i) => `<div class="mascot" style="--d:${i * 0.8}s">${catSVG(n, opts)}</div>`).join('');
}
