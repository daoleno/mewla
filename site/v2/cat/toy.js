// An original designer-toy cat: a big head on a small sitting body, one eye
// peeking. Three finishes share one shape: plush, vinyl, and vinyl in the
// seal's vermilion. Nothing here copies an existing toy character.

const NS = 'http://www.w3.org/2000/svg';

const FINISHES = [
  {
    id: 'plush', name: '毛绒', test: '软 · 可以抱',
    note: '短毛绒的质感，边缘毛茸茸的；一只眼闭着，一只眼偷偷睁开看你。',
    fur: '#f3b46a', hi: '#ffd9a3', shade: '#d38a42', cream: '#fff3e2', ear: '#f5a6a0', line: '#3b2a22', stripe: '#dc8d45',
    fuzz: true, gloss: false,
  },
  {
    id: 'vinyl', name: '搪胶', test: '亮 · 像摆件',
    note: '光滑的搪胶公仔，奶白色，高光很亮；同一个坐姿和表情。',
    fur: '#efe8dc', hi: '#ffffff', shade: '#cdbfaa', cream: '#fffaf1', ear: '#f3b2aa', line: '#3a2c25', stripe: '#c9b8a0',
    fuzz: false, gloss: true,
  },
  {
    id: 'seal', name: '朱红搪胶', test: '接上朱印的颜色',
    note: '和朱印同一个红，脸和肚子是印章里的那种米白；放在页面上和 logo 是一家。',
    fur: '#cf3d2e', hi: '#ec6a55', shade: '#9f2a1f', cream: '#fbf1e2', ear: '#f2a596', line: '#3b1d17', stripe: '#b0301f',
    fuzz: false, gloss: true,
  },
];

const HEAD = 'M 160 64 C 230 64 258 96 258 141 C 258 189 222 214 160 214 C 98 214 62 189 62 141 C 62 96 90 64 160 64 Z';
const EAR_L = 'M 76 114 C 66 82 70 52 84 34 C 108 42 128 60 138 78 Z';
const EAR_L_IN = 'M 86 100 C 82 80 84 62 92 50 C 106 58 116 68 122 80 Z';
const EAR_R = 'M 182 78 C 194 60 214 44 236 36 C 248 54 250 84 244 114 Z';
const EAR_R_IN = 'M 198 80 C 206 68 218 58 230 52 C 236 66 236 84 233 100 Z';

function toy(f, uid) {
  const g = (id) => `url(#${uid}-${id})`;
  const fuzz = f.fuzz ? `filter="url(#${uid}-fuzz)"` : '';
  const fluff = f.fuzz ? `filter="url(#${uid}-soft)"` : '';
  const gloss = f.gloss ? `
      <ellipse cx="112" cy="94" rx="30" ry="13" fill="#fff" opacity=".55" transform="rotate(-28 112 94)"/>
      <ellipse cx="88" cy="128" rx="6" ry="10" fill="#fff" opacity=".35" transform="rotate(12 88 128)"/>` : '';
  const bodyGloss = f.gloss ? `<ellipse cx="118" cy="206" rx="16" ry="7" fill="#fff" opacity=".45" transform="rotate(-24 118 206)"/>` : '';
  return `
  <svg viewBox="0 0 320 300" role="img" aria-label="${f.name}睡猫">
    <defs>
      <radialGradient id="${uid}-skin" cx="38%" cy="30%" r="78%">
        <stop offset="0" stop-color="${f.hi}"/><stop offset=".55" stop-color="${f.fur}"/><stop offset="1" stop-color="${f.shade}"/>
      </radialGradient>
      <radialGradient id="${uid}-cream" cx="45%" cy="35%" r="70%">
        <stop offset="0" stop-color="#fff"/><stop offset="1" stop-color="${f.cream}"/>
      </radialGradient>
      <filter id="${uid}-soft" filterUnits="userSpaceOnUse" x="0" y="0" width="320" height="300">
        <feTurbulence type="fractalNoise" baseFrequency="1.1" numOctaves="2" seed="7" result="n"/>
        <feDisplacementMap in="SourceGraphic" in2="n" scale="3.5" xChannelSelector="R" yChannelSelector="G"/>
      </filter>
      <filter id="${uid}-fuzz" filterUnits="userSpaceOnUse" x="0" y="0" width="320" height="300">
        <feTurbulence type="fractalNoise" baseFrequency="1.1" numOctaves="2" seed="4" result="n"/>
        <feDisplacementMap in="SourceGraphic" in2="n" scale="5" xChannelSelector="R" yChannelSelector="G" result="d"/>
        <feTurbulence type="fractalNoise" baseFrequency="2.4" numOctaves="1" seed="9" result="m"/>
        <feColorMatrix in="m" type="matrix" values="0 0 0 0 0  0 0 0 0 0  0 0 0 0 0  0 0 0 -0.7 0.4" result="dots"/>
        <feComposite in="dots" in2="d" operator="in" result="tex"/>
        <feBlend in="tex" in2="d" mode="multiply"/>
      </filter>
    </defs>
    <ellipse cx="160" cy="276" rx="96" ry="10" fill="rgba(80,50,30,.13)"/>
    <g class="breathe">
      <g class="tail"><path d="M 214 258 C 262 262 278 222 258 194 C 252 186 244 186 242 194" fill="none" stroke="${f.shade}" stroke-width="24" stroke-linecap="round" ${fuzz}/></g>
      <g ${fuzz}>
        <ellipse cx="160" cy="230" rx="76" ry="50" fill="${g('skin')}"/>
      </g>
      <g ${fluff}>
        <ellipse cx="160" cy="240" rx="42" ry="30" fill="${g('cream')}"/>
        <ellipse cx="130" cy="268" rx="19" ry="11" fill="${g('cream')}"/>
        <ellipse cx="190" cy="268" rx="19" ry="11" fill="${g('cream')}"/>
      </g>
      <path d="M 124 268 v 6 M 132 268 v 6 M 184 268 v 6 M 192 268 v 6" stroke="${f.line}" stroke-opacity=".35" stroke-width="2" stroke-linecap="round"/>
      ${bodyGloss}
      <g class="head">
        <g ${fuzz}>
          <path d="${EAR_L}" fill="${g('skin')}"/>
          <path d="${EAR_R}" fill="${g('skin')}"/>
          <path d="${HEAD}" fill="${g('skin')}"/>
        </g>
        <ellipse cx="160" cy="170" rx="62" ry="38" fill="${g('cream')}" ${fluff}/>
        <path d="${EAR_L_IN}" fill="${f.ear}" opacity=".85"/>
        <path d="${EAR_R_IN}" fill="${f.ear}" opacity=".85"/>
        <path d="M 148 74 q 2 12 0 20 M 160 72 v 22 M 172 74 q -2 12 0 20" stroke="${f.stripe}" stroke-width="6" stroke-linecap="round" fill="none"/>
        ${gloss}
        <ellipse cx="103" cy="174" rx="13" ry="7" fill="${f.ear}" opacity=".55"/>
        <ellipse cx="217" cy="174" rx="13" ry="7" fill="${f.ear}" opacity=".55"/>
        <path d="M 104 152 Q 118 163 132 152" fill="none" stroke="${f.line}" stroke-width="4.2" stroke-linecap="round"/>
        <g class="shut"><path d="M 188 152 Q 202 163 216 152" fill="none" stroke="${f.line}" stroke-width="4.2" stroke-linecap="round"/></g>
        <g class="peek">
          <path d="M 187 153 Q 202 145 217 153 Q 202 165 187 153 Z" fill="${f.line}"/>
          <circle cx="206" cy="152" r="2.4" fill="#fff"/>
          <path d="M 185 151 Q 202 142 219 151" fill="none" stroke="${f.line}" stroke-width="3.4" stroke-linecap="round"/>
        </g>
        <path d="M 153 164 Q 160 160 167 164 Q 164 171 160 172 Q 156 171 153 164 Z" fill="#c9746c"/>
        <path d="M 148 177 Q 154 183 160 176 Q 166 183 172 177" fill="none" stroke="${f.line}" stroke-width="2.6" stroke-linecap="round"/>
        <path d="M 164 178.6 L 166.6 185 L 169.2 178.2 Z" fill="#fff" stroke="${f.line}" stroke-width="1.2" stroke-linejoin="round"/>
        <g fill="${f.line}" opacity=".45"><circle cx="132" cy="176" r="1.6"/><circle cx="127" cy="182" r="1.6"/><circle cx="188" cy="176" r="1.6"/><circle cx="193" cy="182" r="1.6"/></g>
      </g>
    </g>
    <g class="zz" fill="${f.shade}" font-family="ui-rounded, 'Arial Rounded MT Bold', system-ui, sans-serif" font-weight="800">
      <text x="250" y="78" font-size="16">z</text><text x="264" y="58" font-size="22">z</text><text x="280" y="34" font-size="28">Z</text>
    </g>
  </svg>`;
}

const grid = document.getElementById('grid');
FINISHES.forEach((f, i) => {
  const card = document.createElement('article');
  card.className = 'card';
  card.innerHTML = `
    <div class="art">${toy(f, `t${i}`)}</div>
    <div class="meta"><span class="num">${String.fromCharCode(65 + i)}</span><span class="name">${f.name}</span><span class="test">${f.test}</span></div>
    <p class="note">${f.note}</p>
    <div class="logos" aria-label="当作图标">
      ${[64, 32, 16].map((px) => `<span class="icon" style="width:${px}px;height:${px}px">${toy(f, `t${i}i${px}`)}</span>`).join('')}
      <span class="lockup"><span class="icon" style="width:30px;height:30px">${toy(f, `t${i}l`)}</span><b>Zen</b></span>
    </div>`;
  grid.appendChild(card);
});
