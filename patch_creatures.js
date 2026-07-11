const fs = require('fs');
let code = fs.readFileSync('static/background.js', 'utf8');

const newCreatures = `
    const CREATURES = {
        sheep: {
            pal: { o: '#e8ecf4', d: '#9aa4c0', e: '#1a2238', p: '#f0b6c8' },
            frames: [
                [
                    '..oooooo..',
                    '.oooooooo.',
                    'ooeooooeoo',
                    'oooooooooo',
                    'opoooooopo',
                    '.oooooooo.',
                    '..d..d.d..',
                    '..d....d..'
                ],
                [
                    '..oooooo..',
                    '.oooooooo.',
                    'ooeooooeoo',
                    'oooooooooo',
                    'opoooooopo',
                    '.oooooooo.',
                    '.d..d..d..',
                    '....d...d.'
                ]
            ]
        },
        fox: {
            pal: { o: '#f2a444', d: '#b06a1e', e: '#1a2238', p: '#ffe3b0' },
            frames: [
                [
                    'o.ooooo...',
                    'oo.oooo...',
                    'oooeoeoo..',
                    'ooooooooo.',
                    'oooooooooo',
                    '.ooooooo..',
                    '..d...d...',
                    '..d...d...'
                ],
                [
                    'o.ooooo...',
                    'oo.oooo...',
                    'oooeoeoo..',
                    'ooooooooo.',
                    'oooooooooo',
                    '.ooooooo..',
                    '.d...d....',
                    '......d...'
                ]
            ]
        },
        grem: {
            pal: { o: '#59d8e6', d: '#2e8fa8', e: '#0c1430', p: '#c9f5fb' },
            frames: [
                [
                    'o......o..',
                    'oo....oo..',
                    'oooooooo..',
                    'ooeooeoo..',
                    '.oooooo...',
                    '..dddd....',
                    '..d..d....',
                    '..d..d....'
                ],
                [
                    '.o....o...',
                    '.oo..oo...',
                    '.oooooo...',
                    '.ooeoeo...',
                    '..oooo....',
                    '...dd.....',
                    '..d..d....',
                    '..d..d....'
                ]
            ]
        },
        shade: {
            pal: { o: '#8f7bd8', d: '#5a4a99', e: '#120a2e', p: '#d8cdfa' },
            frames: [
                [
                    '...oooo...',
                    '..oooooo..',
                    '.ooeooeoo.',
                    '.oooooooo.',
                    '.oooooooo.',
                    '..oooooo..',
                    '...o..o...',
                    '...o..o...'
                ],
                [
                    '...oooo...',
                    '..oooooo..',
                    '.ooeooeoo.',
                    '.oooooooo.',
                    '.oooooooo.',
                    '..oooooo..',
                    '..o....o..',
                    '..........'
                ]
            ]
        },
        boar: {
            pal: { o: '#8b5a2b', d: '#5c3a21', e: '#1a1100', p: '#d2b48c' },
            frames: [
                [
                    '..........',
                    '...oooo...',
                    '.ooooooo..',
                    'poeooooeoo',
                    'oooooooooo',
                    '.oooooooo.',
                    '..d....d..',
                    '..d....d..'
                ],
                [
                    '..........',
                    '...oooo...',
                    '.ooooooo..',
                    'poeooooeoo',
                    'oooooooooo',
                    '.oooooooo.',
                    '.d....d...',
                    '.......d..'
                ]
            ]
        },
        peng: {
            pal: { o: '#3b82f6', d: '#1d4ed8', e: '#0f172a', p: '#f8fafc' },
            frames: [
                [
                    '...oooo...',
                    '..oooooo..',
                    '.ooeooeoo.',
                    '.oopppoo..',
                    '.oopppoo..',
                    '.oooppoo..',
                    '...d..d...',
                    '...d..d...'
                ],
                [
                    '...oooo...',
                    '..oooooo..',
                    '.ooeooeoo.',
                    '.oopppoo..',
                    '.oopppoo..',
                    '.oooppoo..',
                    '..d..d....',
                    '......d...'
                ]
            ]
        },
        dragon: {
            pal: { o: '#ef4444', d: '#991b1b', e: '#fef08a', p: '#fca5a5' },
            frames: [
                [
                    '.p....p...',
                    '.oooooo...',
                    'ooeooeoo..',
                    'oooooooooo',
                    '.oooooooo.',
                    '..ooooo...',
                    '..d...d...',
                    '..d...d...'
                ],
                [
                    '..p..p....',
                    '..oooo....',
                    'poeooeoo..',
                    'oooooooooo',
                    '.oooooooo.',
                    '..ooooo...',
                    '.d...d....',
                    '......d...'
                ]
            ]
        },
        frog: {
            pal: { o: '#22c55e', d: '#15803d', e: '#022c22', p: '#86efac' },
            frames: [
                [
                    '..........',
                    '..........',
                    '..oo..oo..',
                    '.ooeooeeo.',
                    '.oooooooo.',
                    'oooooooooo',
                    'd.d....d.d',
                    '..........'
                ],
                [
                    '..........',
                    '..oo..oo..',
                    '.ooeooeeo.',
                    '.oooooooo.',
                    'oooooooooo',
                    'd.d....d.d',
                    'd........d',
                    '..........'
                ]
            ]
        },
        cat: {
            pal: { o: '#52525b', d: '#27272a', e: '#fde047', p: '#a1a1aa' },
            frames: [
                [
                    '.o....o...',
                    '.oooooo...',
                    '.ooeooeo..',
                    '..oooooo.o',
                    '..oooooo.o',
                    '...oooo..o',
                    '...d..d...',
                    '...d..d...'
                ],
                [
                    '..o...o...',
                    '..ooooo...',
                    '.ooeooeo.o',
                    '..oooooo.o',
                    '..oooooo.o',
                    '...oooo...',
                    '..d..d....',
                    '......d...'
                ]
            ]
        },
        stag: {
            pal: { o: '#d6d3d1', d: '#78716c', e: '#1c1917', p: '#fafaf9' },
            frames: [
                [
                    'p.p..p.p..',
                    '.pp..pp...',
                    '..oooo....',
                    '.ooeoeo...',
                    '..oooo....',
                    '..oooo....',
                    '...d..d...',
                    '...d..d...'
                ],
                [
                    'p.p..p.p..',
                    '.pp..pp...',
                    '..oooo....',
                    '.ooeoeo...',
                    '..oooo....',
                    '..oooo....',
                    '..d..d....',
                    '.......d..'
                ]
            ]
        }
    };
    const species = Object.keys(CREATURES);
`;

code = code.replace(/const PALETTES = \{[\s\S]*?const MONSTER_FRAMES = \[[\s\S]*?\];\n/, newCreatures);

// Replace any leftover `const species = Object.keys(PALETTES);`
code = code.replace(/const species = Object\.keys\(PALETTES\);\n/, '');

// Fix drawMonster to use CREATURES
const drawMonsterOld = /function drawMonster\(m, time\) \{[\s\S]*?m\.dir < 0\);\n    \}/;
const drawMonsterNew = `function drawMonster(m, time) {
        const p = project(m.xNorm, m.depth);
        const px = Math.max(2, Math.round(7 * p.scale)) * m.shrink * Math.min(1, m.pop);
        if (px <= 0.1) return;
        const bob = Math.sin(time * 6 + m.phase) * 3 * p.scale;
        
        const creature = CREATURES[m.kind];
        const frame = creature.frames[Math.floor((time * 6 + m.phase)) % 2];
        ctx.fillStyle = 'rgba(0,0,0,0.35)';
        ctx.beginPath();
        ctx.ellipse(p.x, p.y + 2, 18 * p.scale * m.shrink, 4 * p.scale * m.shrink, 0, 0, Math.PI * 2);
        ctx.fill();
        drawSprite(frame, creature.pal, p.x, p.y + bob, px, m.dir < 0);
    }`;
code = code.replace(drawMonsterOld, drawMonsterNew);

fs.writeFileSync('static/background.js', code);
console.log('Creatures patched!');
