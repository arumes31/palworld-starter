const fs = require('fs');
let code = fs.readFileSync('static/background.js', 'utf8');

const newPalettes = `    const PALETTES = {
        sheep:  { o: '#e8ecf4', d: '#9aa4c0', e: '#1a2238', p: '#f0b6c8' },
        fox:    { o: '#f2a444', d: '#b06a1e', e: '#1a2238', p: '#ffe3b0' },
        grem:   { o: '#59d8e6', d: '#2e8fa8', e: '#0c1430', p: '#c9f5fb' },
        shade:  { o: '#8f7bd8', d: '#5a4a99', e: '#120a2e', p: '#d8cdfa' },
        boar:   { o: '#8b5a2b', d: '#5c3a21', e: '#1a1100', p: '#d2b48c' },
        peng:   { o: '#3b82f6', d: '#1d4ed8', e: '#0f172a', p: '#f8fafc' },
        dragon: { o: '#ef4444', d: '#991b1b', e: '#fef08a', p: '#fca5a5' },
        frog:   { o: '#22c55e', d: '#15803d', e: '#022c22', p: '#86efac' },
        cat:    { o: '#52525b', d: '#27272a', e: '#fde047', p: '#a1a1aa' },
        stag:   { o: '#d6d3d1', d: '#78716c', e: '#1c1917', p: '#fafaf9' }
    };`;

code = code.replace(/const PALETTES = \{[\s\S]*?\};\n/, newPalettes + "\n");

const playerDefs = `    const PLAYER_FRAMES = [
        [
            '...hhh...',
            '..hhhhh..',
            '.ppEppp..',
            '.sssssss.',
            '.sssssss.',
            '..pp.pp..',
            '..ll.ll..',
            '..l...l..'
        ],
        [
            '...hhh...',
            '..hhhhh..',
            '.ppEppp..',
            '..sssss..',
            '..sssss..',
            '...ppp...',
            '...lll...',
            '...l.l...'
        ]
    ];

    const player = {
        xNorm: 0.5,
        depth: 0.5,
        dir: 1,
        speed: 0.08,
        phase: 0,
        state: 'roam',
        timer: 0
    };
`;

code = code.replace(/const species = Object\.keys\(PALETTES\);/, playerDefs + "\n    const species = Object.keys(PALETTES);");

code = code.replace(/for \(let i = 0; i < 9; i\+\+\) spawnMonster\(true\);/, 'for (let i = 0; i < 14; i++) spawnMonster(true);');

const throwBallOld = /function throwBall\(\) \{[\s\S]*?timer: 0\n        \}\);\n    \}/;
const throwBallNew = `function throwBall() {
        const free = monsters.filter(m => m.state === 'roam' && m.pop >= 1);
        if (!free.length) return;
        const target = free[Math.floor(Math.random() * free.length)];
        target.state = 'targeted';

        player.dir = target.xNorm > player.xNorm ? 1 : -1;
        player.state = 'throw';
        player.timer = 0.8;

        const pp = project(player.xNorm, player.depth);
        const px = pp.x;
        const py = pp.y - 14 * pp.scale;

        balls.push({
            target: target,
            t: 0,
            duration: rnd(0.7, 1.0),
            startX: px,
            startY: py,
            spin: 0,
            state: 'flying',
            timer: 0
        });
    }`;
code = code.replace(throwBallOld, throwBallNew);

const drawMonsterOld = /function drawMonster\(m, time\) \{[\s\S]*?m\.dir < 0\);\n    \}/;
const drawMonsterAndPlayer = `function drawMonster(m, time) {
        const p = project(m.xNorm, m.depth);
        const px = Math.max(2, Math.round(7 * p.scale)) * m.shrink * Math.min(1, m.pop);
        if (px <= 0.1) return;
        const bob = Math.sin(time * 6 + m.phase) * 3 * p.scale;
        const frame = MONSTER_FRAMES[Math.floor((time * 6 + m.phase)) % 2];
        ctx.fillStyle = 'rgba(0,0,0,0.35)';
        ctx.beginPath();
        ctx.ellipse(p.x, p.y + 2, 18 * p.scale * m.shrink, 4 * p.scale * m.shrink, 0, 0, Math.PI * 2);
        ctx.fill();
        drawSprite(frame, PALETTES[m.kind], p.x, p.y + bob, px, m.dir < 0);
    }

    function drawPlayer(time) {
        const p = project(player.xNorm, player.depth);
        const px = Math.max(2, Math.round(7 * p.scale));
        if (px <= 0.1) return;
        
        let bob = 0;
        let frameIdx = 0;
        if (player.state === 'roam') {
            bob = Math.sin(time * 8 + player.phase) * 3 * p.scale;
            frameIdx = Math.floor(time * 8 + player.phase) % 2;
        } else if (player.state === 'throw') {
            frameIdx = 0;
        }
        
        const frame = PLAYER_FRAMES[frameIdx];
        ctx.fillStyle = 'rgba(0,0,0,0.35)';
        ctx.beginPath();
        ctx.ellipse(p.x, p.y + 2, 16 * p.scale, 4 * p.scale, 0, 0, Math.PI * 2);
        ctx.fill();

        const pal = {
            'h': '#0f172a',
            'E': '#ffffff',
            's': '#2563eb',
            'l': '#451a03',
            'p': '#fcd34d'
        };
        drawSprite(frame, pal, p.x, p.y + bob, px, player.dir < 0);
    }`;
code = code.replace(drawMonsterOld, drawMonsterAndPlayer);


const stepOld = /function step\(dt, time\) \{[\s\S]*?for \(const m of monsters\)/;
const stepNew = `function step(dt, time) {
        if (player.state === 'roam') {
            player.xNorm += player.dir * player.speed * dt;
            player.depth += Math.sin(time * 0.5 + player.phase) * 0.02 * dt;
            player.depth = Math.max(0.18, Math.min(0.96, player.depth));
            if (player.xNorm < 0.05 || player.xNorm > 0.95) {
                player.dir *= -1;
                player.xNorm = Math.max(0.05, Math.min(0.95, player.xNorm));
            }
            if (Math.random() < dt * 0.2) player.dir *= -1;
        } else if (player.state === 'throw') {
            player.timer -= dt;
            if (player.timer <= 0) player.state = 'roam';
        }

        for (const m of monsters)`;
code = code.replace(stepOld, stepNew);


const renderOld = /const sorted = monsters\.slice\(\)\.sort\(\(a, b\) => a\.depth - b\.depth\);\n        for \(const m of sorted\) \{\n            if \(m\.state !== 'gone'\) drawMonster\(m, time\);\n        \}/;
const renderNew = `const sorted = monsters.slice();
        sorted.push({ isPlayer: true, depth: player.depth });
        sorted.sort((a, b) => a.depth - b.depth);
        for (const m of sorted) {
            if (m.isPlayer) {
                drawPlayer(time);
            } else if (m.state !== 'gone') {
                drawMonster(m, time);
            }
        }`;
code = code.replace(renderOld, renderNew);

fs.writeFileSync('static/background.js', code);
console.log("Patched background.js");
