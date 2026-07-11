const fs = require('fs');
let code = fs.readFileSync('static/background.js', 'utf8');

// Replace PLAYER_FRAMES
const oldPlayerFrames = /const PLAYER_FRAMES = \[\s*\[[\s\S]*?\]\n    \];/;
const newPlayerFrames = `const PLAYER_FRAMES = [
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
    ];`;
code = code.replace(oldPlayerFrames, newPlayerFrames);

// Update drawPlayer to use frameIdx = 2 for throw
const oldDrawPlayer = /} else if \(player\.state === 'throw'\) {\n            frameIdx = 0;\n        }/;
const newDrawPlayer = `} else if (player.state === 'throw') {
            frameIdx = 2; // Throwing animation frame
        }`;
code = code.replace(oldDrawPlayer, newDrawPlayer);

// Update throwBall startY
const oldThrowBallY = /const py = pp\.y - 14 \* pp\.scale;/;
const newThrowBallY = `const py = pp.y - 38 * pp.scale; // Start from hand height`;
code = code.replace(oldThrowBallY, newThrowBallY);

fs.writeFileSync('static/background.js', code);
console.log('Player throw animation patched!');
