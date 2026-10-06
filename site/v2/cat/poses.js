// Sleeping poses, drawn in one plain flat style so only the pose is being judged.

const C = {
  fur: '#f4ac63', far: '#e3954e', stripe: '#d27a36', cream: '#fff3e0', creamFar: '#f1dcc0',
  line: '#4b3427', pink: '#f2a7a2', shadow: 'rgba(75,52,39,.13)',
};
const LW = 2.6;
const base = `stroke="${C.line}" stroke-width="${LW}" stroke-linejoin="round" stroke-linecap="round"`;

const shape = (d, fill = C.fur) => `<path d="${d}" fill="${fill}" ${base}/>`;
const line = (d, w = 2) => `<path d="${d}" fill="none" stroke="${C.line}" stroke-width="${w}" stroke-linecap="round"/>`;
const ellipse = (x, y, rx, ry, fill = C.fur, rot = 0) => `<ellipse cx="${x}" cy="${y}" rx="${rx}" ry="${ry}" transform="rotate(${rot} ${x} ${y})" fill="${fill}" ${base}/>`;
const shadow = (x, y, rx, ry) => `<ellipse cx="${x}" cy="${y}" rx="${rx}" ry="${ry}" fill="${C.shadow}"/>`;
const stripes = (ds, w = 5) => ds.map((d) => `<path d="${d}" fill="none" stroke="${C.stripe}" stroke-width="${w}" stroke-linecap="round"/>`).join('');
let clipN = 0;
const clipped = (d, inner) => { const id = `k${clipN++}`; return `<clipPath id="${id}"><path d="${d}"/></clipPath><g clip-path="url(#${id})">${inner}</g>`; };

// A round tube with ringed stripes: tails and legs.
function tube(d, w, { fill = C.fur, rings = false, cap = 'round' } = {}) {
  return `<path d="${d}" fill="none" stroke="${C.line}" stroke-width="${w + LW * 2}" stroke-linecap="${cap}" stroke-linejoin="round"/>`
    + `<path d="${d}" fill="none" stroke="${fill}" stroke-width="${w}" stroke-linecap="${cap}" stroke-linejoin="round"/>`
    + (rings ? `<path d="${d}" fill="none" stroke="${C.stripe}" stroke-width="${w - 1}" stroke-dasharray="5 13" stroke-dashoffset="6"/>` : '');
}
function leg(d, x, y, w = 16, { far = false, rot = 0 } = {}) {
  return tube(d, w, { fill: far ? C.far : C.fur }) + ellipse(x, y, w * 0.72, w * 0.5, far ? C.creamFar : C.cream, rot);
}

// The head, drawn upright around (0,0) and placed with a turn and a scale.
function head(x, y, { rot = 0, s = 1, far = false } = {}) {
  const ear = (m) => `<path d="M ${-38 * m} -12 L ${-35 * m} -58 L ${-8 * m} -39 Z" fill="${C.fur}" ${base}/>`
    + `<path d="M ${-31 * m} -20 L ${-30 * m} -47 L ${-15 * m} -36 Z" fill="${C.pink}"/>`;
  const whisk = [-1, 1].map((m) => line(`M ${14 * m} 15 Q ${32 * m} 11 ${54 * m} 12`, 1.2) + line(`M ${14 * m} 19 Q ${32 * m} 20 ${52 * m} 26`, 1.2)).join('');
  return `<g transform="translate(${x} ${y}) rotate(${rot}) scale(${s})">${ear(1)}${ear(-1)}`
    + `<ellipse cx="0" cy="0" rx="46" ry="40" fill="${C.fur}" ${base}/>`
    + stripes(['M -9 -37 L -7 -26', 'M 0 -39 L 0 -27', 'M 9 -37 L 7 -26', 'M -45 -2 L -34 0', 'M -44 7 L -35 7', 'M 45 -2 L 34 0', 'M 44 7 L 35 7'], 3.2)
    + `<ellipse cx="-7" cy="15" rx="9.5" ry="6.5" fill="${C.cream}"/><ellipse cx="7" cy="15" rx="9.5" ry="6.5" fill="${C.cream}"/><ellipse cx="0" cy="25" rx="13" ry="7" fill="${C.cream}"/>`
    + line('M -27 2 Q -18 9 -9 2', 2.4) + line('M 9 2 Q 18 9 27 2', 2.4)
    + `<path d="M -4.5 9.5 Q 0 8 4.5 9.5 L 0 14 Z" fill="${C.pink}" stroke="${C.line}" stroke-width="1" stroke-linejoin="round"/>`
    + line('M 0 14 L 0 17 M -6 17 Q -3 20.5 0 17 Q 3 20.5 6 17', 1.5)
    + whisk + '</g>';
}

const zzz = (x, y, s = 1) => `<g fill="${C.line}" opacity=".55" font-family="ui-rounded, 'SF Pro Rounded', system-ui, sans-serif" font-weight="700">`
  + `<text x="${x}" y="${y}" font-size="${12 * s}">z</text><text x="${x + 12 * s}" y="${y - 12 * s}" font-size="${16 * s}">z</text><text x="${x + 28 * s}" y="${y - 28 * s}" font-size="${21 * s}">Z</text></g>`;

const POSES = [
  {
    name: '猫团子', note: '整只团成一个球，脸埋进身体，尾巴从前面绕过来盖住下巴。',
    svg() {
      const body = 'M 74 204 C 64 132 130 82 214 82 C 302 82 352 136 344 198 C 340 228 302 240 206 240 C 118 240 80 228 74 204 Z';
      return shadow(206, 242, 150, 11)
        + shape(body)
        + clipped(body, stripes(['M 214 90 C 210 104 212 116 218 128', 'M 250 92 C 252 106 256 116 264 124', 'M 180 96 C 174 108 174 120 180 130', 'M 284 104 C 290 116 296 124 306 128', 'M 150 108 C 142 118 140 128 144 138'], 6))
        + line('M 262 114 C 316 126 334 176 314 220', 2.2)
        + ellipse(214, 228, 20, 10, C.cream)
        + head(152, 178, { rot: -16, s: 0.95 })
        + tube('M 340 204 C 332 238 272 250 206 248 C 154 246 116 240 96 222', 24, { rings: true })
        + zzz(318, 84);
    },
  },
  {
    name: '揣手', note: '像一条面包：四只脚都收在身子底下，头立着在打盹，尾巴贴着身侧。',
    svg() {
      const body = 'M 112 238 C 100 176 144 126 224 124 C 306 122 346 166 348 238 Z';
      return shadow(228, 242, 140, 10)
        + shape(body)
        + clipped(body, stripes(['M 214 128 C 210 146 212 160 218 172', 'M 246 126 C 246 144 250 158 258 168', 'M 278 132 C 282 148 288 160 298 168', 'M 306 146 C 314 158 322 166 332 172', 'M 330 176 C 336 184 342 190 348 194'], 7) + `<ellipse cx="130" cy="206" rx="26" ry="34" fill="${C.cream}"/>`)
        + ellipse(140, 240, 14, 6, C.cream) + ellipse(166, 242, 14, 6, C.cream)
        + tube('M 344 226 C 340 248 300 252 240 252 C 200 252 178 250 170 246', 18, { rings: true })
        + head(132, 146, { rot: -8 })
        + zzz(186, 92);
    },
  },
  {
    name: '侧躺摊开', note: '侧身一倒，四条腿伸直，肚皮朝外，头歪在地上。',
    svg() {
      const body = 'M 118 190 C 118 150 170 138 222 140 C 280 142 318 160 318 192 C 318 222 276 232 220 232 C 160 232 118 222 118 190 Z';
      return shadow(220, 244, 170, 10)
        + tube('M 310 186 C 350 174 378 194 372 230', 18, { rings: true })
        + leg('M 282 214 C 300 226 314 236 330 242', 330, 242, 16, { far: true, rot: 30 })
        + leg('M 150 214 C 130 226 108 236 90 242', 88, 242, 16, { far: true, rot: -30 })
        + shape(body)
        + clipped(body, stripes(['M 180 144 C 178 156 180 166 184 174', 'M 212 140 C 212 154 214 164 218 172', 'M 244 142 C 246 154 250 164 256 170', 'M 274 148 C 280 158 286 166 294 170'], 7) + `<ellipse cx="222" cy="222" rx="80" ry="22" fill="${C.cream}"/>`)
        + leg('M 296 206 C 318 216 336 226 352 234', 354, 234, 17, { rot: 28 })
        + leg('M 156 206 C 136 218 116 228 98 232', 94, 232, 17, { rot: -24 })
        + head(100, 182, { rot: -28, s: 0.92 })
        + zzz(326, 120);
    },
  },
  {
    name: '肚皮朝天', note: '仰面躺平，四只小爪子蜷在空中，头倒过来，毫无防备。',
    svg() {
      const body = 'M 128 200 C 128 164 176 150 224 152 C 280 154 316 172 316 202 C 316 228 272 236 222 236 C 166 236 128 228 128 200 Z';
      return shadow(224, 244, 160, 10)
        + tube('M 312 210 C 348 222 372 210 378 184', 17, { rings: true })
        + leg('M 186 168 C 186 148 184 134 192 120', 194, 117, 15, { far: true, rot: -10 })
        + leg('M 288 176 C 302 160 318 154 332 154', 336, 154, 15, { far: true, rot: 70 })
        + shape(body)
        + clipped(body, `<ellipse cx="226" cy="168" rx="80" ry="24" fill="${C.cream}"/>` + stripes(['M 190 232 C 192 222 196 214 202 208', 'M 228 236 C 228 226 230 218 234 212', 'M 266 232 C 266 222 268 216 272 210'], 6))
        + leg('M 160 174 C 154 154 150 138 156 124', 156, 120, 16, { rot: 6 })
        + leg('M 266 168 C 274 148 288 136 300 130', 304, 127, 16, { rot: 60 })
        + head(104, 196, { rot: 156, s: 0.92 })
        + zzz(330, 110);
    },
  },
  {
    name: '背影小山', note: '背对着你团成一座小山，只露出两只耳尖和绕在前面的尾巴。',
    svg() {
      const body = 'M 92 238 C 80 152 140 96 214 96 C 292 96 346 150 338 238 Z';
      return shadow(214, 242, 140, 10)
        + `<path d="M 146 112 L 138 74 L 174 96 Z" fill="${C.fur}" ${base}/><path d="M 182 98 L 196 64 L 212 100 Z" fill="${C.fur}" ${base}/>`
        + shape(body)
        // tabby marks run down both sides from the spine
        + clipped(body, stripes([0, 1, 2, 3].flatMap((k) => {
          const y = 116 + k * 30, l = 34 + k * 14;
          return [`M 206 ${y} C ${206 - l * 0.5} ${y + 2} ${206 - l} ${y + 10} ${206 - l * 1.3} ${y + 22}`, `M 222 ${y} C ${222 + l * 0.5} ${y + 2} ${222 + l} ${y + 10} ${222 + l * 1.3} ${y + 22}`];
        }), 7) + stripes(['M 214 100 L 214 220'], 4).replace('stroke-width="4"', 'stroke-width="4" opacity=".55"'))
        + tube('M 334 226 C 326 252 256 256 196 254 C 146 252 110 248 100 232', 20, { rings: true })
        + zzz(290, 92);
    },
  },
  {
    name: '坐着打盹', note: '坐得端端正正，脑袋却一点一点往下垂，尾巴绕在前脚上。',
    svg() {
      const body = 'M 150 238 C 134 196 150 150 200 140 C 250 150 266 196 250 238 Z';
      return shadow(204, 244, 100, 9)
        + ellipse(158, 214, 28, 26) + ellipse(242, 214, 28, 26)
        + shape(body)
        + clipped(body, `<ellipse cx="200" cy="186" rx="28" ry="40" fill="${C.cream}"/>` + stripes(['M 158 180 L 170 184', 'M 156 198 L 168 200', 'M 242 180 L 230 184', 'M 244 198 L 232 200'], 5))
        + tube('M 186 186 L 186 230', 17) + tube('M 214 186 L 214 230', 17)
        + ellipse(186, 236, 12, 7, C.cream) + ellipse(214, 236, 12, 7, C.cream)
        + tube('M 250 230 C 294 236 292 254 244 252 C 214 252 196 250 176 248', 16, { rings: true })
        + head(198, 128, { rot: 16, s: 0.98 })
        + zzz(250, 72);
    },
  },
  {
    name: '伸长了睡', note: '整只拉成长条，前爪往前伸，下巴贴地，后腿往后蹬。',
    svg() {
      const body = 'M 138 212 C 138 186 190 176 240 178 C 296 180 336 190 336 214 C 336 234 296 238 238 238 C 180 238 138 234 138 212 Z';
      return shadow(222, 246, 180, 9)
        + tube('M 330 200 C 356 186 382 194 384 172', 16, { rings: true })
        + leg('M 300 226 C 326 232 346 238 364 242', 368, 242, 15, { far: true, rot: 10 })
        + leg('M 160 226 C 130 232 100 238 76 242', 72, 242, 15, { far: true, rot: -6 })
        + shape(body)
        + clipped(body, stripes(['M 200 180 C 198 192 200 200 204 206', 'M 232 178 C 232 190 234 198 238 204', 'M 264 180 C 266 190 270 198 274 202', 'M 294 184 C 298 192 304 198 310 202'], 6) + `<ellipse cx="236" cy="234" rx="80" ry="12" fill="${C.cream}"/>`)
        + leg('M 312 220 C 336 226 356 230 376 232', 380, 232, 16, { rot: 6 })
        + leg('M 168 220 C 136 224 106 228 80 230', 76, 230, 16, { rot: -4 })
        + head(122, 208, { rot: -6, s: 0.86 })
        + zzz(156, 140);
    },
  },
  {
    name: '睡在笔记本上', note: '摊在你的笔记本键盘上，一只爪子垂下来，尾巴挂在边上，屏幕还亮着。',
    svg() {
      const screen = '<rect x="104" y="58" width="196" height="150" rx="10" fill="#2b3138" stroke="#2b3138" stroke-width="2"/>'
        + '<rect x="114" y="68" width="176" height="128" rx="4" fill="#1d252e"/>'
        + [[124, 82, 60, '#7fd1a8'], [132, 96, 90, '#8fb8ff'], [132, 110, 44, '#f4c27a'], [124, 124, 70, '#7fd1a8'], [132, 138, 100, '#c9a7ff'], [124, 152, 52, '#8fb8ff']]
          .map(([x, y, w, c]) => `<rect x="${x}" y="${y}" width="${w}" height="5" rx="2.5" fill="${c}" opacity=".85"/>`).join('')
        + '<rect x="180" y="166" width="7" height="12" fill="#e9eef5" opacity=".9"/>';
      const deck = '<path d="M 84 208 L 320 208 L 346 232 L 58 232 Z" fill="#d6dbe2" stroke="#8a929c" stroke-width="2" stroke-linejoin="round"/>'
        + '<path d="M 58 232 L 346 232 L 346 237 Q 202 241 58 237 Z" fill="#b7bec8"/>';
      const body = 'M 112 222 C 112 192 160 176 222 176 C 290 176 326 194 326 222 Z';
      return shadow(202, 246, 160, 8) + screen + deck
        + tube('M 318 218 C 338 226 346 244 342 266', 15, { rings: true })
        + shape(body)
        + clipped(body, stripes(['M 196 178 C 194 190 196 200 200 208', 'M 228 176 C 228 188 230 198 234 206', 'M 260 178 C 262 190 266 198 272 204', 'M 290 186 C 296 194 302 202 310 206'], 6))
        + leg('M 150 222 C 148 236 146 248 146 258', 146, 261, 15, { rot: 80 })
        + head(122, 196, { rot: -74, s: 0.78 })
        + zzz(318, 140);
    },
  },
];

const grid = document.getElementById('grid');
POSES.forEach((p, i) => {
  const card = document.createElement('article');
  card.className = 'card';
  card.innerHTML = `<svg viewBox="0 0 400 280" role="img" aria-label="${p.name}">${p.svg()}</svg>
    <div class="meta"><span class="num">${String.fromCharCode(65 + i)}</span><span class="name">${p.name}</span></div>
    <p class="note">${p.note}</p>`;
  grid.appendChild(card);
});
