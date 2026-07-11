// Fully animated background scene: a living pixel-art night landscape where
// monsters wander on a 3D-projected plane and get captured by thrown spheres
// (Palworld style). Pure canvas 2D with perspective projection - no external
// libraries. Always animates (decorative site background by design); pauses
// while the tab is hidden to save battery.
(function () {
    'use strict';

    const canvas = document.getElementById('bg-scene');
    if (!canvas) return;
    const ctx = canvas.getContext('2d');

    function rnd(a, b) { return a + Math.random() * (b - a); }

    // ==================== PIXEL SPRITES ====================
        
    const CREATURES = {
        sheep: {
            behavior: 'wander',
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
            behavior: 'dart',
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
            behavior: 'pace',
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
            behavior: 'float',
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
            behavior: 'charge',
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
            behavior: 'waddle',
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
            behavior: 'soar',
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
            behavior: 'hop',
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
            behavior: 'dart',
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
            behavior: 'pace',
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

    function drawSprite(frame, palette, x, y, px, flip) {
        const h = frame.length, w = frame[0].length;
        for (let row = 0; row < h; row++) {
            for (let col = 0; col < w; col++) {
                const ch = frame[row][flip ? w - 1 - col : col];
                if (ch === '.') continue;
                ctx.fillStyle = palette[ch];
                ctx.fillRect(x + (col - w / 2) * px, y + (row - h) * px, px + 0.5, px + 0.5);
            }
        }
    }

    // ==================== WORLD / PROJECTION ====================
    let W = 0, H = 0, horizon = 0;
    let stars = [], mountainsFar = [], mountainsNear = [], grass = [], fireflies = [], clouds = [];
    let skyGrad = null, groundGrad = null, vignette = null;

    function buildWorld() {
        stars = [];
        const starCount = Math.floor(W / 9);
        for (let i = 0; i < starCount; i++) {
            stars.push({
                x: Math.random() * W,
                y: Math.random() * horizon * 0.95,
                r: rnd(0.5, 1.6),
                phase: rnd(0, Math.PI * 2),
                speed: rnd(0.6, 2.2)
            });
        }

        function ridge(baseY, rough, step) {
            const pts = [];
            let y = baseY + rnd(-rough, rough);
            for (let x = -step; x <= W + step; x += step) {
                y += rnd(-rough, rough);
                y = Math.max(baseY - rough * 3, Math.min(baseY + rough * 2, y));
                pts.push({ x: x, y: y });
            }
            return pts;
        }
        mountainsFar = ridge(horizon * 0.72, 14, 90);
        mountainsNear = ridge(horizon * 0.9, 22, 70);

        grass = [];
        for (let i = 0; i < 90; i++) {
            grass.push({ xNorm: Math.random(), depth: Math.random(), phase: rnd(0, Math.PI * 2) });
        }

        fireflies = [];
        for (let i = 0; i < 14; i++) {
            fireflies.push({
                xNorm: Math.random(),
                depth: rnd(0.2, 0.95),
                phase: rnd(0, Math.PI * 2),
                speed: rnd(0.3, 0.8)
            });
        }

        clouds = [];
        for (let i = 0; i < 5; i++) {
            clouds.push({
                x: Math.random() * W,
                y: rnd(horizon * 0.15, horizon * 0.75),
                w: rnd(W * 0.18, W * 0.4),
                h: rnd(14, 34),
                speed: rnd(3, 10),
                alpha: rnd(0.04, 0.1)
            });
        }

        skyGrad = ctx.createLinearGradient(0, 0, 0, horizon);
        skyGrad.addColorStop(0, '#050b1c');
        skyGrad.addColorStop(0.65, '#0a1323');
        skyGrad.addColorStop(1, '#12203a');

        groundGrad = ctx.createLinearGradient(0, horizon, 0, H);
        groundGrad.addColorStop(0, '#101d33');
        groundGrad.addColorStop(0.35, '#0b1527');
        groundGrad.addColorStop(1, '#050e1e');

        vignette = ctx.createRadialGradient(W / 2, H * 0.55, Math.min(W, H) * 0.35, W / 2, H * 0.55, Math.max(W, H) * 0.8);
        vignette.addColorStop(0, 'rgba(5,14,30,0)');
        vignette.addColorStop(1, 'rgba(3,8,18,0.55)');
    }

    function resize() {
        W = canvas.width = window.innerWidth;
        H = canvas.height = window.innerHeight;
        horizon = H * 0.42;
        ctx.imageSmoothingEnabled = false;
        buildWorld();
    }
    window.addEventListener('resize', resize);
    resize();

    // depth: 0 = far (horizon), 1 = near (bottom edge)
    function project(xNorm, depth) {
        const persp = 0.16 + 0.84 * depth * depth;
        return {
            x: W / 2 + (xNorm - 0.5) * W * (0.4 + 0.75 * persp),
            y: horizon + (H - horizon) * persp,
            scale: persp
        };
    }

    // ==================== ENTITIES ====================
        const PLAYER_FRAMES = [
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
        ],
        [
            '...hhh...',
            '..hhhhh..',
            '.ppEppp.p',
            '..ssssspp',
            '..sssss..',
            '...ppp...',
            '..l...l..',
            '..l...l..'
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

        const monsters = [];
    const balls = [];
    const particles = [];

    function spawnMonster(initial) {
        monsters.push({
            kind: species[Math.floor(Math.random() * species.length)],
            xNorm: rnd(0.05, 0.95),
            depth: rnd(0.2, 0.95),
            dir: Math.random() < 0.5 ? -1 : 1,
            speed: rnd(0.025, 0.055),
            phase: rnd(0, Math.PI * 2),
            state: 'roam',
            shrink: 1,
            pop: initial ? 1 : 0
        });
    }
    for (let i = 0; i < 14; i++) spawnMonster(true);

    function throwBall() {
        const free = monsters.filter(m => m.state === 'roam' && m.pop >= 1);
        if (!free.length) return;
        let target = free[0];
        let minDist = Math.abs(target.xNorm - player.xNorm);
        for (let i = 1; i < free.length; i++) {
            const dist = Math.abs(free[i].xNorm - player.xNorm);
            if (dist < minDist) {
                minDist = dist;
                target = free[i];
            }
        }
        target.state = 'targeted';

        player.dir = target.xNorm > player.xNorm ? 1 : -1;
        player.state = 'throw';
        player.timer = 0.8;

        const pp = project(player.xNorm, player.depth);
        const px = pp.x;
        const py = pp.y - 38 * pp.scale; // Start from hand height

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
    }

    function burst(x, y, scale, color) {
        for (let i = 0; i < 16; i++) {
            const a = (Math.PI * 2 * i) / 16 + rnd(-0.2, 0.2);
            particles.push({
                x: x, y: y,
                vx: Math.cos(a) * rnd(40, 150) * scale,
                vy: Math.sin(a) * rnd(40, 150) * scale - 50 * scale,
                life: rnd(0.4, 1),
                age: 0,
                size: rnd(2, 5) * scale + 1,
                color: color
            });
        }
    }

    // ==================== DRAWING ====================
    function drawSky(time) {
        ctx.fillStyle = skyGrad;
        ctx.fillRect(0, 0, W, horizon);

        for (const s of stars) {
            const tw = 0.35 + 0.65 * (0.5 + 0.5 * Math.sin(time * s.speed + s.phase));
            ctx.globalAlpha = tw;
            ctx.fillStyle = '#dbeafe';
            ctx.fillRect(s.x, s.y, s.r, s.r);
        }
        ctx.globalAlpha = 1;

        // moon with glow
        const mx = W * 0.78, my = horizon * 0.28, mr = Math.min(W, H) * 0.035;
        const glow = ctx.createRadialGradient(mx, my, mr * 0.4, mx, my, mr * 4);
        glow.addColorStop(0, 'rgba(219,252,255,0.35)');
        glow.addColorStop(1, 'rgba(219,252,255,0)');
        ctx.fillStyle = glow;
        ctx.fillRect(mx - mr * 4, my - mr * 4, mr * 8, mr * 8);
        ctx.fillStyle = '#e6f6f8';
        ctx.beginPath();
        ctx.arc(mx, my, mr, 0, Math.PI * 2);
        ctx.fill();
        ctx.fillStyle = 'rgba(160,196,210,0.5)';
        ctx.beginPath();
        ctx.arc(mx - mr * 0.3, my - mr * 0.2, mr * 0.22, 0, Math.PI * 2);
        ctx.arc(mx + mr * 0.25, my + mr * 0.3, mr * 0.15, 0, Math.PI * 2);
        ctx.fill();
    }

    function drawClouds(dt) {
        for (const c of clouds) {
            c.x += c.speed * dt;
            if (c.x - c.w > W) c.x = -c.w;
            ctx.globalAlpha = c.alpha;
            ctx.fillStyle = '#9fc2e8';
            ctx.beginPath();
            ctx.ellipse(c.x, c.y, c.w / 2, c.h, 0, 0, Math.PI * 2);
            ctx.fill();
        }
        ctx.globalAlpha = 1;
    }

    function drawRidge(pts, color) {
        ctx.fillStyle = color;
        ctx.beginPath();
        ctx.moveTo(pts[0].x, pts[0].y);
        for (const p of pts) ctx.lineTo(p.x, p.y);
        ctx.lineTo(W + 100, horizon + 2);
        ctx.lineTo(-100, horizon + 2);
        ctx.closePath();
        ctx.fill();
    }

    function drawGround(time) {
        ctx.fillStyle = groundGrad;
        ctx.fillRect(0, horizon, W, H - horizon);

        // perspective grid
        ctx.strokeStyle = 'rgba(0, 240, 255, 0.05)';
        ctx.lineWidth = 1;
        for (let i = 0; i <= 12; i++) {
            const xn = i / 12;
            const a = project(xn, 0.02), b = project(xn, 1);
            ctx.beginPath();
            ctx.moveTo(a.x, a.y);
            ctx.lineTo(b.x, b.y);
            ctx.stroke();
        }
        for (let i = 1; i <= 7; i++) {
            const p = project(0, i / 7);
            ctx.beginPath();
            ctx.moveTo(0, p.y);
            ctx.lineTo(W, p.y);
            ctx.stroke();
        }

        // swaying grass tufts
        for (const g of grass) {
            const p = project(g.xNorm, g.depth);
            const s = Math.max(1.5, 5 * p.scale);
            const sway = Math.sin(time * 1.5 + g.phase) * s * 0.4;
            ctx.strokeStyle = 'rgba(46, 143, 120, 0.5)';
            ctx.lineWidth = Math.max(1, p.scale * 1.5);
            ctx.beginPath();
            ctx.moveTo(p.x, p.y);
            ctx.lineTo(p.x + sway, p.y - s * 2);
            ctx.moveTo(p.x + s * 0.8, p.y);
            ctx.lineTo(p.x + s * 0.8 + sway, p.y - s * 1.5);
            ctx.stroke();
        }
    }

    function drawFireflies(time) {
        for (const f of fireflies) {
            const p = project(
                f.xNorm + Math.sin(time * f.speed + f.phase) * 0.02,
                f.depth + Math.cos(time * f.speed * 0.7 + f.phase) * 0.02
            );
            const y = p.y - (24 + 14 * Math.sin(time * f.speed + f.phase * 2)) * p.scale;
            const a = 0.3 + 0.5 * (0.5 + 0.5 * Math.sin(time * 2.2 + f.phase));
            ctx.globalAlpha = a;
            ctx.fillStyle = '#7df4ff';
            ctx.fillRect(p.x, y, 2 + p.scale, 2 + p.scale);
        }
        ctx.globalAlpha = 1;
    }

    function drawBallSprite(x, y, r, rot) {
        ctx.save();
        ctx.translate(x, y);
        ctx.rotate(rot);
        ctx.fillStyle = '#c8d2e4';
        ctx.beginPath();
        ctx.arc(0, 0, r, 0, Math.PI);
        ctx.fill();
        ctx.fillStyle = '#0d5f8f';
        ctx.beginPath();
        ctx.arc(0, 0, r, Math.PI, Math.PI * 2);
        ctx.fill();
        ctx.fillStyle = '#0a1323';
        ctx.fillRect(-r, -r * 0.14, r * 2, r * 0.28);
        ctx.fillStyle = '#00f0ff';
        ctx.beginPath();
        ctx.arc(0, 0, r * 0.22, 0, Math.PI * 2);
        ctx.fill();
        ctx.restore();
    }

    function drawMonster(m, time) {
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
            frameIdx = 2; // Throwing animation frame
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
    }

    // ==================== SIMULATION ====================
    let nextThrow = 1.5;

    function step(dt, time) {
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

        for (const m of monsters) {
            if (m.pop < 1) m.pop = Math.min(1, m.pop + dt * 2);
            if (m.state === 'roam' || m.state === 'targeted') {
                const b = CREATURES[m.kind].behavior;
                
                let dx = m.dir * m.speed * dt;
                let dy = 0;
                
                if (b === 'wander') {
                    dy = Math.sin(time * 0.4 + m.phase) * 0.01 * dt;
                    if (Math.random() < dt * 0.5) m.dir *= -1;
                } else if (b === 'dart') {
                    // Stop and go
                    if (Math.sin(time * 3 + m.phase) > 0) dx = 0;
                    else dx *= 2;
                    dy = Math.sin(time * 0.2 + m.phase) * 0.005 * dt;
                } else if (b === 'float') {
                    // Moves slowly, bobs up and down a lot in depth
                    dx *= 0.5;
                    dy = Math.sin(time * 0.8 + m.phase) * 0.03 * dt;
                } else if (b === 'charge') {
                    // Paces slowly, sometimes charges fast
                    if (Math.sin(time * 1.5 + m.phase) > 0.8) dx *= 3.5;
                    else dx *= 0.5;
                    dy = Math.cos(time * 0.3 + m.phase) * 0.01 * dt;
                } else if (b === 'waddle') {
                    // Very slow, high bob
                    dx *= 0.4;
                    dy = Math.sin(time * 2 + m.phase) * 0.015 * dt;
                } else if (b === 'soar') {
                    // Very fast, long sweeping arcs in depth
                    dx *= 1.5;
                    dy = Math.cos(time * 0.5 + m.phase) * 0.04 * dt;
                } else if (b === 'hop') {
                    // Stop completely, then jump forward fast
                    if (Math.sin(time * 4 + m.phase) > 0.5) dx *= 2.5;
                    else dx = 0;
                    dy = Math.cos(time * 0.4 + m.phase) * 0.01 * dt;
                } else {
                    // Default pace
                    dy = Math.sin(time * 0.4 + m.phase) * 0.01 * dt;
                    if (Math.random() < dt * 0.2) m.dir *= -1;
                }
                
                m.xNorm += dx;
                m.depth += dy;
                
                m.depth = Math.max(0.18, Math.min(0.96, m.depth));
                if (m.xNorm < 0.04 || m.xNorm > 0.96) {
                    m.dir *= -1;
                    m.xNorm = Math.max(0.04, Math.min(0.96, m.xNorm));
                }
            }
        }

        nextThrow -= dt;
        if (nextThrow <= 0 && balls.length < 2) {
            throwBall();
            nextThrow = rnd(4, 8);
        }

        for (const b of balls) {
            const tp = project(b.target.xNorm, b.target.depth);
            if (b.state === 'flying') {
                b.t += dt / b.duration;
                b.spin += dt * 14;
                if (b.t >= 1) {
                    b.state = 'absorbing';
                    b.timer = 0;
                    b.target.state = 'shrinking';
                    burst(tp.x, tp.y - 14 * tp.scale, tp.scale, 'rgba(0,240,255,0.9)');
                }
            } else if (b.state === 'absorbing') {
                b.timer += dt;
                b.target.shrink = Math.max(0, 1 - b.timer / 0.55);
                if (b.timer >= 0.55) {
                    b.target.state = 'gone';
                    b.state = 'wobbling';
                    b.timer = 0;
                }
            } else if (b.state === 'wobbling') {
                b.timer += dt;
                if (b.timer >= 2.2) {
                    b.state = 'burst';
                    burst(tp.x, tp.y - 8 * tp.scale, tp.scale, 'rgba(255,219,157,0.95)');
                    const m = b.target;
                    setTimeout(() => {
                        m.xNorm = rnd(0.05, 0.95);
                        m.depth = rnd(0.2, 0.95);
                        m.shrink = 1;
                        m.pop = 0;
                        m.state = 'roam';
                    }, rnd(2000, 5000));
                }
            }
        }
        for (let i = balls.length - 1; i >= 0; i--) {
            if (balls[i].state === 'burst') balls.splice(i, 1);
        }

        for (let i = particles.length - 1; i >= 0; i--) {
            const pt = particles[i];
            pt.age += dt;
            if (pt.age >= pt.life) { particles.splice(i, 1); continue; }
            pt.x += pt.vx * dt;
            pt.y += pt.vy * dt;
            pt.vy += 180 * dt;
        }
    }

    function render(dt, time) {
        drawSky(time);
        drawClouds(dt);
        drawRidge(mountainsFar, '#0c1830');
        drawRidge(mountainsNear, '#081222');
        drawGround(time);
        drawFireflies(time);

        const sorted = monsters.slice();
        sorted.push({ isPlayer: true, depth: player.depth });
        sorted.sort((a, b) => a.depth - b.depth);
        for (const m of sorted) {
            if (m.isPlayer) {
                drawPlayer(time);
            } else if (m.state !== 'gone') {
                drawMonster(m, time);
            }
        }

        for (const b of balls) {
            const tp = project(b.target.xNorm, b.target.depth);
            const r = Math.max(4, 11 * tp.scale);
            if (b.state === 'flying') {
                const t = Math.min(1, b.t);
                const x = b.startX + (tp.x - b.startX) * t;
                const arc = Math.sin(t * Math.PI) * H * 0.25;
                const y = b.startY + (tp.y - 14 * tp.scale - b.startY) * t - arc;
                drawBallSprite(x, y, r * (1.4 - 0.4 * t), b.spin);
            } else if (b.state === 'absorbing') {
                drawBallSprite(tp.x, tp.y - 8 * tp.scale, r, 0);
            } else if (b.state === 'wobbling') {
                const rot = Math.sin(b.timer * 9) * 0.35 * Math.max(0, 1 - b.timer / 2.2);
                drawBallSprite(tp.x, tp.y - 8 * tp.scale, r, rot);
            }
        }

        for (const pt of particles) {
            ctx.globalAlpha = Math.max(0, 1 - pt.age / pt.life);
            ctx.fillStyle = pt.color;
            ctx.fillRect(pt.x - pt.size / 2, pt.y - pt.size / 2, pt.size, pt.size);
        }
        ctx.globalAlpha = 1;

        ctx.fillStyle = vignette;
        ctx.fillRect(0, 0, W, H);
    }

    // ==================== LOOP ====================
    let last = performance.now();
    function loop(now) {
        requestAnimationFrame(loop);
        if (document.hidden) { last = now; return; }
        const dt = Math.min(0.05, (now - last) / 1000);
        last = now;
        step(dt, now / 1000);
        render(dt, now / 1000);
    }
    requestAnimationFrame(loop);
})();
