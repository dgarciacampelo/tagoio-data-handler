// Store current context
let currentStation = { url: '', pool: '', name: '' };

function openQrModal(url, poolCode, stationName) {
    currentStation = { url, pool: poolCode, name: stationName };

    document.getElementById('modal-title').textContent = `${poolCode} / ${stationName}`;
    document.getElementById('qr-url-text').textContent = url;

    // Generate QR on the canvas
    const canvas = document.getElementById('qr-canvas');
    QRCode.toCanvas(canvas, url, {
        width: 250,
        margin: 2,
        color: {
            dark: '#c3bf48',  // Primary brand color
            light: '#ffffff'
        }
    }, function (error) {
        if (error) console.error("Error generating QR:", error);
    });

    // Show modal
    const modal = document.getElementById('qr-modal');
    const content = document.getElementById('qr-modal-content');
    modal.classList.remove('hidden');

    // Trigger reflow for transition
    void modal.offsetWidth;
    modal.classList.remove('opacity-0');
    content.classList.remove('scale-95');
}

function closeQrModal() {
    const modal = document.getElementById('qr-modal');
    const content = document.getElementById('qr-modal-content');

    modal.classList.add('opacity-0');
    content.classList.add('scale-95');

    setTimeout(() => { modal.classList.add('hidden'); }, 300); // Matches Tailwind transition duration
}

function downloadPNG() {
    const canvas = document.getElementById('qr-canvas');
    const link = document.createElement('a');
    link.download = `VeloEnergy_QR_${currentStation.pool}_${currentStation.name}.png`;
    link.href = canvas.toDataURL('image/png');
    link.click();
}

function printPoster() {
    // Convert canvas to image data URL for the print window
    const canvas = document.getElementById('qr-canvas');
    const qrDataUrl = canvas.toDataURL('image/png');

    // Create a new invisible iframe or window for printing
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
                .qr-image { width: 400px; height: 400px; }
                .info-box { margin-top: 40px; text-align: center; font-size: 1.8rem; color: #4a4a4a; }
                .bold { font-weight: bold; color: #1a1a1a; }
            </style>
        </head>
        <body>
            <div class="header">Velo Energy</div>
            <div class="subtitle">Escanea para Recargar</div>
            
            <div class="qr-container">
                <img class="qr-image" src="${qrDataUrl}" alt="Código QR del punto de recarga" />
            </div>
            
            <div class="info-box">
                <p>Grupo: <span class="bold">${currentStation.pool}</span></p>
                <p>Punto de Recarga: <span class="bold">${currentStation.name}</span></p>
            </div>
            
            <script>
                // Wait for image to load before triggering print
                window.onload = function() {
                    window.print();
                    // Optional: close the window after printing
                    // setTimeout(() => window.close(), 500);
                };
            </script>
        </body>
        </html>
    `;

    printWindow.document.open();
    printWindow.document.write(htmlContent);
    printWindow.document.close();
}

// Close modal on outside click
document.getElementById('qr-modal').addEventListener('click', function (e) {
    if (e.target === this) {
        closeQrModal();
    }
});