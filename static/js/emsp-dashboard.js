// Store current context
let currentStation = { url: '', pool: '', name: '', cpo: '' };

// Helper function to extract data from the HTML button safely
function openQrModalFromData(buttonElement) {
    const url = buttonElement.dataset.url;
    const poolCode = buttonElement.dataset.pool;
    const stationName = buttonElement.dataset.station;
    const cpoName = buttonElement.dataset.cpo;

    openQrModal(url, poolCode, stationName, cpoName);
}

function openQrModal(url, poolCode, stationName, cpoName) {
    currentStation = { url, pool: poolCode, name: stationName, cpo: cpoName };

    // Update Modal UI
    document.getElementById('modal-cpo-name').textContent = cpoName;
    document.getElementById('modal-title').textContent = `${poolCode} / ${stationName}`;
    document.getElementById('qr-url-text').textContent = url;

    const scale = 3; // 3x resolution for ultra-crisp text and print-quality PNG
    const logicalWidth = 280;
    const logicalHeight = 320;

    // Generate QR Data URL at the scaled-up resolution, for a sharper QR code image
    QRCode.toDataURL(url, {
        width: logicalWidth * scale,
        margin: 2,
        color: { dark: '#00288e', light: '#ffffff' }
    }, function (error, dataUrl) {
        if (error) {
            console.error("Error generating QR:", error);
            return;
        }

        const canvas = document.getElementById('qr-canvas');
        const ctx = canvas.getContext('2d');

        // 1. Set the actual physical pixel count of the canvas (High Res)
        canvas.width = logicalWidth * scale;
        canvas.height = logicalHeight * scale;

        // 2. Set the CSS display size to keep it looking normal on the screen
        canvas.style.width = logicalWidth + 'px';
        canvas.style.height = logicalHeight + 'px';

        // 3. Scale the drawing context. This lets us use normal coordinates (e.g. 280) 
        // while the browser automatically draws it at the higher resolution.
        ctx.scale(scale, scale);

        // Draw white background
        ctx.fillStyle = '#ffffff';
        ctx.fillRect(0, 0, logicalWidth, logicalHeight);

        // Draw Title Text (Will be rendered crisply due to ctx.scale)
        ctx.fillStyle = '#00288e';
        ctx.font = 'bold 14px "Segoe UI", Tahoma, sans-serif';
        ctx.textAlign = 'center';
        ctx.fillText(`Velo Energy: ${poolCode} / ${stationName}`, logicalWidth / 2, 24);

        // Load the High-Res QR Image and draw it below the text
        const img = new Image();
        // Draw into the 280x280 logical box. The scaled context handles the high-res mapping.
        img.onload = function () { ctx.drawImage(img, 0, 35, logicalWidth, logicalWidth); };
        img.src = dataUrl;
    });

    // Show modal
    const modal = document.getElementById('qr-modal');
    const content = document.getElementById('qr-modal-content');
    modal.classList.remove('hidden');

    void modal.offsetWidth;
    modal.classList.remove('opacity-0');
    content.classList.remove('scale-95');
}

function closeQrModal() {
    const modal = document.getElementById('qr-modal');
    const content = document.getElementById('qr-modal-content');

    modal.classList.add('opacity-0');
    content.classList.add('scale-95');

    setTimeout(() => { modal.classList.add('hidden'); }, 300);
}

// Because we scaled the canvas above, this will download a beautiful 840x960 PNG
function downloadPNG() {
    const canvas = document.getElementById('qr-canvas');
    const link = document.createElement('a');
    link.download = `VeloEnergy_QR_${currentStation.pool}_${currentStation.name}.png`;
    link.href = canvas.toDataURL('image/png');
    link.click();
}

function printPoster() {
    const canvas = document.getElementById('qr-canvas');
    const qrDataUrl = canvas.toDataURL('image/png');
    const printWindow = window.open('', '_blank');

    const htmlContent = `
        <!DOCTYPE html>
        <html lang="es">
        <head>
            <title>Cartel QR - ${currentStation.name}</title>
            <style>
                @media print {
                    @page { size: A4 portrait; margin: 0; }
                    body { -webkit-print-color-adjust: exact; print-color-adjust: exact; }
                }
                body {
                    margin: 0; padding: 0;
                    display: flex; flex-direction: column; align-items: center; justify-content: center;
                    height: 100vh; font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
                    background-color: white; color: #1a1a1a;
                }
                .header { color: #00288e; font-size: 3.5rem; font-weight: bold; margin-bottom: 10px; }
                .subtitle { font-size: 2.2rem; color: #4a4a4a; margin-top: 0; margin-bottom: 40px; }
                .qr-container { padding: 20px; border: 4px solid #00288e; border-radius: 24px; display: inline-block; }
                
                /* FIXED: Set height to auto to prevent aspect ratio stretching */
                .qr-image { width: 350px; height: auto; } 
                
                .info-box { margin-top: 40px; text-align: center; font-size: 1.8rem; color: #4a4a4a; line-height: 1.5; }
                .bold { font-weight: bold; color: #1a1a1a; }
            </style>
        </head>
        <body>
            <div class="header">Velo Energy</div>
            <div class="subtitle">Escanea para Recargar</div>
            
            <div class="qr-container">
                <img class="qr-image" src="${qrDataUrl}" alt="Código QR de la estación" />
            </div>
            
            <div class="info-box">
                <p>Operador (CPO): <span class="bold">${currentStation.cpo}</span></p>
                <p>Grupo: <span class="bold">${currentStation.pool}</span></p>
                <p>Estación: <span class="bold">${currentStation.name}</span></p>
            </div>
            
            <script>
                window.onload = function() {
                    window.print();
                };
            </script>
        </body>
        </html>
    `;

    printWindow.document.open();
    printWindow.document.write(htmlContent);
    printWindow.document.close();
}

document.getElementById('qr-modal').addEventListener('click', function (e) {
    if (e.target === this) { closeQrModal(); }
});