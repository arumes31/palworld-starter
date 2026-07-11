// Scratch-card overlay for captcha number images. Each .scratch-wrap holds
// the number <img> and a <canvas> cover the visitor rubs free. The submit
// button unlocks once every cover is sufficiently scratched.
(function () {
    'use strict';

    function initScratch() {
        const wraps = document.querySelectorAll('.scratch-wrap');
        if (!wraps.length) return;

        const submitBtn = document.querySelector('.captcha-form button[type="submit"]');
        const lang = document.documentElement.lang || 'en';
        const label = lang === 'de' ? 'rubbeln' : 'scratch';
        const done = new Set();

        if (submitBtn) submitBtn.disabled = true;

        function maybeUnlock() {
            if (done.size === wraps.length && submitBtn) {
                submitBtn.disabled = false;
            }
        }

        wraps.forEach(function (wrap) {
            const img = wrap.querySelector('.scratch-img');
            const canvas = wrap.querySelector('.scratch-cover');

            function setup() {
                const w = img.clientWidth || img.naturalWidth;
                const h = img.clientHeight || img.naturalHeight;
                if (!w || !h) return;
                canvas.width = w;
                canvas.height = h;
                const ctx = canvas.getContext('2d');

                ctx.fillStyle = '#66738f';
                ctx.fillRect(0, 0, w, h);
                ctx.fillStyle = 'rgba(255,255,255,0.8)';
                ctx.font = '11px sans-serif';
                ctx.textAlign = 'center';
                ctx.textBaseline = 'middle';
                ctx.fillText(label, w / 2, h / 2);

                let scratching = false;
                let moves = 0;

                function checkProgress() {
                    const data = ctx.getImageData(0, 0, w, h).data;
                    let clear = 0, total = 0;
                    for (let i = 3; i < data.length; i += 4 * 17) {
                        total++;
                        if (data[i] === 0) clear++;
                    }
                    if (total > 0 && clear / total > 0.10) {
                        canvas.classList.add('scratched');
                        done.add(wrap);
                        maybeUnlock();
                    }
                }

                function scratchAt(clientX, clientY) {
                    const rect = canvas.getBoundingClientRect();
                    ctx.globalCompositeOperation = 'destination-out';
                    ctx.beginPath();
                    ctx.arc(clientX - rect.left, clientY - rect.top, 13, 0, Math.PI * 2);
                    ctx.fill();
                    if (++moves % 5 === 0) checkProgress();
                }

                canvas.addEventListener('pointerdown', function (e) {
                    scratching = true;
                    canvas.setPointerCapture(e.pointerId);
                    scratchAt(e.clientX, e.clientY);
                    e.preventDefault();
                });
                canvas.addEventListener('pointermove', function (e) {
                    if (scratching) scratchAt(e.clientX, e.clientY);
                });
                canvas.addEventListener('pointerup', function () {
                    scratching = false;
                    checkProgress();
                });
                canvas.addEventListener('pointercancel', function () {
                    scratching = false;
                });
            }

            if (img.complete && img.naturalWidth > 0) {
                setup();
            } else {
                img.addEventListener('load', setup);
            }
        });
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', initScratch);
    } else {
        initScratch();
    }
})();
