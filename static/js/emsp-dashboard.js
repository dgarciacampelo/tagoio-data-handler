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
        color: { dark: '#00571b', light: '#ffffff' }
    }, function (error, dataUrl) {
        if (error) {
            console.error("Error generating QR:", error);
            return;
        }

        // Store the dataUrl in the global context for the print function
        currentStation.qrDataUrl = dataUrl;

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
        ctx.fillStyle = '#00571b';
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
    // 1. Get the raw HTML string from the template
    const templateElement = document.getElementById('qr-print-template');
    let htmlContent = templateElement.innerHTML;

    // 2. Replace the placeholders with the current modal data
    htmlContent = htmlContent
        .replace(/__STATION_NAME__/g, currentStation.name)
        .replace(/__QR_DATA_URL__/g, currentStation.qrDataUrl)
        .replace(/__CPO_NAME__/g, currentStation.cpo)
        .replace(/__POOL_NAME__/g, currentStation.pool);

    // 3. Open the new window and write the injected HTML
    const printWindow = window.open('', '_blank');

    if (printWindow) {
        printWindow.document.open();
        printWindow.document.write(htmlContent);
        printWindow.document.close(); // Crucial: tells the browser writing is finished so it can fire window.onload
    } else {
        alert("Por favor, permita las ventanas emergentes (pop-ups) para imprimir el cartel.");
    }
}

document.getElementById('qr-modal').addEventListener('click', function (e) {
    if (e.target === this) { closeQrModal(); }
});