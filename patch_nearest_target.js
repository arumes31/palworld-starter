const fs = require('fs');
let code = fs.readFileSync('static/background.js', 'utf8');

const oldThrowBall = /const target = free\[Math\.floor\(Math\.random\(\) \* free\.length\)\];/;
const newThrowBall = `let target = free[0];
        let minDist = Math.abs(target.xNorm - player.xNorm);
        for (let i = 1; i < free.length; i++) {
            const dist = Math.abs(free[i].xNorm - player.xNorm);
            if (dist < minDist) {
                minDist = dist;
                target = free[i];
            }
        }`;

code = code.replace(oldThrowBall, newThrowBall);

fs.writeFileSync('static/background.js', code);
console.log('Nearest target patched!');
