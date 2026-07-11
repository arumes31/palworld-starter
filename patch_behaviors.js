const fs = require('fs');
let code = fs.readFileSync('static/background.js', 'utf8');

// Add behaviors
code = code.replace(/sheep: \{/, "sheep: {\n            behavior: 'wander',");
code = code.replace(/fox: \{/, "fox: {\n            behavior: 'dart',");
code = code.replace(/grem: \{/, "grem: {\n            behavior: 'pace',");
code = code.replace(/shade: \{/, "shade: {\n            behavior: 'float',");
code = code.replace(/boar: \{/, "boar: {\n            behavior: 'charge',");
code = code.replace(/peng: \{/, "peng: {\n            behavior: 'waddle',");
code = code.replace(/dragon: \{/, "dragon: {\n            behavior: 'soar',");
code = code.replace(/frog: \{/, "frog: {\n            behavior: 'hop',");
code = code.replace(/cat: \{/, "cat: {\n            behavior: 'dart',");
code = code.replace(/stag: \{/, "stag: {\n            behavior: 'pace',");

// Update step logic for monsters
const oldStepLoop = /for \(const m of monsters\) \{\n            if \(m\.pop < 1\) m\.pop = Math\.min\(1, m\.pop \+ dt \* 2\);\n            if \(m\.state === 'roam' \|\| m\.state === 'targeted'\) \{\n                m\.xNorm \+= m\.dir \* m\.speed \* dt;\n                m\.depth \+= Math\.sin\(time \* 0\.4 \+ m\.phase\) \* 0\.01 \* dt;\n                m\.depth = Math\.max\(0\.18, Math\.min\(0\.96, m\.depth\)\);\n                if \(m\.xNorm < 0\.04 \|\| m\.xNorm > 0\.96\) \{\n                    m\.dir \*= -1;\n                    m\.xNorm = Math\.max\(0\.04, Math\.min\(0\.96, m\.xNorm\)\);\n                \}\n                if \(Math\.random\(\) < dt \* 0\.2\) m\.dir \*= -1;\n            \}\n        \}/;

const newStepLoop = `for (const m of monsters) {
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
        }`;

code = code.replace(oldStepLoop, newStepLoop);

fs.writeFileSync('static/background.js', code);
console.log('Behaviors patched!');
