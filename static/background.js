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
    const PALETTES = {
        sheep: { o: '#e8ecf4', d: '#9aa4c0', e: '#1a2238', p: '#f0b6c8' },
        fox:   { o: '#f2a444', d: '#b06a1e', e: '#1a2238', p: '#ffe3b0' },
        grem:  { o: '#59d8e6', d: '#2e8fa8', e: '#0c1430', p: '#c9f5fb' },
        shade: { o: '#8f7bd8', d: '#5a4a99', e: '#120a2e', p: '#d8cdfa' }
    };

    const MONSTER_FRAMES = [
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
    ];

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
    const species = Object.keys(PALETTES);
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
    for (let i = 0; i < 9; i++) spawnMonster(true);

    function throwBall() {
        const free = monsters.filter(m => m.state === 'roam' && m.pop >= 1);
        if (!free.length) return;
        const target = free[Math.floor(Math.random() * free.length)];
        target.state = 'targeted';
        const fromLeft = Math.random() < 0.5;
        balls.push({
            target: target,
            t: 0,
            duration: rnd(0.9, 1.3),
            startX: fromLeft ? -40 : W + 40,
            startY: H * rnd(0.7, 0.95),
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
        const frame = MONSTER_FRAMES[Math.floor((time * 6 + m.phase)) % 2];
        ctx.fillStyle = 'rgba(0,0,0,0.35)';
        ctx.beginPath();
        ctx.ellipse(p.x, p.y + 2, 18 * p.scale * m.shrink, 4 * p.scale * m.shrink, 0, 0, Math.PI * 2);
        ctx.fill();
        drawSprite(frame, PALETTES[m.kind], p.x, p.y + bob, px, m.dir < 0);
    }

    // ==================== SIMULATION ====================
    let nextThrow = 1.5;

    function step(dt, time) {
        for (const m of monsters) {
            if (m.pop < 1) m.pop = Math.min(1, m.pop + dt * 2);
            if (m.state === 'roam' || m.state === 'targeted') {
                m.xNorm += m.dir * m.speed * dt;
                m.depth += Math.sin(time * 0.4 + m.phase) * 0.01 * dt;
                m.depth = Math.max(0.18, Math.min(0.96, m.depth));
                if (m.xNorm < 0.04 || m.xNorm > 0.96) {
                    m.dir *= -1;
                    m.xNorm = Math.max(0.04, Math.min(0.96, m.xNorm));
                }
                if (Math.random() < dt * 0.2) m.dir *= -1;
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

        const sorted = monsters.slice().sort((a, b) => a.depth - b.depth);
        for (const m of sorted) {
            if (m.state !== 'gone') drawMonster(m, time);
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
