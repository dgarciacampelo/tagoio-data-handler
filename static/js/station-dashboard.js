// 1. Global state to preserve form data across 5s HTMX OOB swaps
window.chargingFormState = window.chargingFormState || {
    email: '',
    needReceipt: false,
    nif: '',
    billingName: '',
    billingAddress: '',
    invoiceEmail: ''
};

function initFormLogic() {
    const form = document.getElementById('charging-form');
    // If the station isn't in PREPARING, the form doesn't exist, so we abort.
    // Note: We intentionally removed the dataset.initialized check so this re-runs after every OOB swap.
    if (!form) return;

    const receiptToggle = document.getElementById('need-receipt');
    const receiptFieldsContainer = document.getElementById('receipt-fields');
    const receiptInputs = document.querySelectorAll('.receipt-input');
    const submitBtn = document.getElementById('submit-btn');
    const btnIcon = document.getElementById('btn-icon');
    const btnText = document.getElementById('btn-text');
    const successState = document.getElementById('success-state');
    const errorBanner = document.getElementById('error-banner');
    const layoutGrid = document.getElementById('main-layout-grid');

    // Function to toggle button state based on native HTML5 validation
    // Returns true if email is valid AND (if receipt is checked) all required receipt fields are filled
    const checkFormValidity = () => { submitBtn.disabled = !form.checkValidity(); };

    // 2. Rehydrate State
    document.getElementById('email').value = window.chargingFormState.email;
    receiptToggle.checked = window.chargingFormState.needReceipt;
    document.getElementById('nif').value = window.chargingFormState.nif;
    document.getElementById('billing_name').value = window.chargingFormState.billingName;
    document.getElementById('billing_address').value = window.chargingFormState.billingAddress;
    document.getElementById('invoice_email').value = window.chargingFormState.invoiceEmail;

    // Apply visual grid state based on rehydration
    if (receiptToggle.checked) {
        receiptFieldsContainer.classList.remove('hidden');
        receiptFieldsContainer.classList.add('flex');
        receiptInputs.forEach(input => input.required = true);
        if (layoutGrid) {
            layoutGrid.classList.remove('items-stretch');
            layoutGrid.classList.add('items-start');
        }
    } else {
        receiptFieldsContainer.classList.add('hidden');
        receiptFieldsContainer.classList.remove('flex');
        receiptInputs.forEach(input => input.required = false);
        if (layoutGrid) {
            layoutGrid.classList.add('items-stretch');
            layoutGrid.classList.remove('items-start');
        }
    }

    // Run validation immediately after rehydrating the form (in case HTMX just swapped it)
    checkFormValidity();

    // 3. Bind State Updaters
    document.getElementById('email').addEventListener('input', e => window.chargingFormState.email = e.target.value);
    document.getElementById('nif').addEventListener('input', e => window.chargingFormState.nif = e.target.value);
    document.getElementById('billing_name').addEventListener('input', e => window.chargingFormState.billingName = e.target.value);
    document.getElementById('billing_address').addEventListener('input', e => window.chargingFormState.billingAddress = e.target.value);
    document.getElementById('invoice_email').addEventListener('input', e => window.chargingFormState.invoiceEmail = e.target.value);

    // Listen to all form input events to check validity in real-time
    form.addEventListener('input', checkFormValidity);

    // 4. Handle Checkbox Toggle
    receiptToggle.addEventListener('change', (e) => {
        const isChecked = e.target.checked;
        window.chargingFormState.needReceipt = isChecked;

        if (isChecked) {
            receiptFieldsContainer.classList.remove('hidden');
            receiptFieldsContainer.classList.add('flex');
            receiptInputs.forEach(input => input.required = true);
            if (layoutGrid) {
                layoutGrid.classList.remove('items-stretch');
                layoutGrid.classList.add('items-start');
            }
        } else {
            receiptFieldsContainer.classList.add('hidden');
            receiptFieldsContainer.classList.remove('flex');
            receiptInputs.forEach(input => input.required = false);
            if (layoutGrid) {
                layoutGrid.classList.add('items-stretch');
                layoutGrid.classList.remove('items-start');
            }
        }

        // Re-run validation because the number of required fields just changed
        checkFormValidity();
    });

    // 5. Form Submission
    form.addEventListener('submit', async (e) => {
        e.preventDefault();

        submitBtn.disabled = true;
        btnIcon.textContent = 'hourglass_empty';
        btnIcon.classList.add('animate-spin');
        btnText.textContent = 'PROCESANDO...';
        errorBanner.classList.add('hidden');

        const payload = {
            pool_code: parseInt(document.getElementById('pool_code').value, 10),
            station_name: document.getElementById('station_name').value,
            connector_id: parseInt(document.getElementById('connector_id').value, 10),
            email: document.getElementById('email').value,
            requires_invoice: receiptToggle.checked,
            nif: document.getElementById('nif').value || null,
            billing_name: document.getElementById('billing_name').value || null,
            billing_address: document.getElementById('billing_address').value || null,
            invoice_email: document.getElementById('invoice_email').value || null
        };

        try {
            const response = await fetch('/api/charge-request', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload)
            });

            if (!response.ok) throw new Error('Network response was not ok');

            form.classList.add('hidden');
            successState.classList.remove('hidden');
            successState.classList.add('flex');

            // Wipe state on success so it doesn't linger if they charge again later
            window.chargingFormState = { email: '', needReceipt: false, nif: '', billingName: '', billingAddress: '', invoiceEmail: '' };

        } catch (error) {
            console.error('Submission failed:', error);
            errorBanner.classList.remove('hidden');
            submitBtn.disabled = false;
            btnIcon.textContent = 'payments';
            btnIcon.classList.remove('animate-spin');
            btnText.textContent = 'ENVIAR ENLACE DE PAGO';
        }
    });
}

function initTabsLogic() {
    // Grab all elements with the 'js-tab-link' class
    const tabLinks = document.querySelectorAll('.js-tab-link');

    tabLinks.forEach(link => {
        link.addEventListener('click', function (e) {
            e.preventDefault(); // Prevents the browser from jumping to the top of the page due to href="#"

            const tabId = this.getAttribute('data-tab-target');

            // Hide all tab contents
            document.querySelectorAll('.tab-content').forEach(el => {
                el.classList.add('hidden');
                el.classList.remove('block');
            });

            // Show the targeted tab
            const targetTab = document.getElementById('tab-' + tabId);
            if (targetTab) {
                targetTab.classList.remove('hidden');
                targetTab.classList.add('block');
            }

            // Reset all nav links to default styling
            tabLinks.forEach(el => {
                el.classList.remove('text-primary', 'font-bold', 'scale-95');
                el.classList.add('text-on-surface-variant');
                const icon = el.querySelector('.material-symbols-outlined');
                if (icon) icon.style.fontVariationSettings = "'FILL' 0";
            });

            // Apply active styling to the clicked link
            this.classList.remove('text-on-surface-variant');
            this.classList.add('text-primary', 'font-bold', 'scale-95');
            const activeIcon = this.querySelector('.material-symbols-outlined');
            if (activeIcon) activeIcon.style.fontVariationSettings = "'FILL' 1";
        });
    });
}

// Initialize form logic on standard page load and after HTMX background swaps
document.addEventListener('DOMContentLoaded', () => {
    initFormLogic();
    initTabsLogic();
});
// Usually, you don't need to re-init tabs on htmx:load unless the nav bar itself is swapped by HTMX.
// If the nav is static, calling initTabsLogic() once on DOMContentLoaded is enough.
document.addEventListener('htmx:load', initFormLogic);

// Global cleanup: If the car unplugs and the form disappears entirely, ensure the grid is reset.
document.addEventListener('htmx:oobAfterSwap', (e) => {
    if (e.detail.target.id === 'side-panel-container') {
        if (!document.getElementById('charging-form')) {
            const layoutGrid = document.getElementById('main-layout-grid');
            if (layoutGrid) {
                layoutGrid.classList.add('items-stretch');
                layoutGrid.classList.remove('items-start');
            }
        }
    }
});

// Listen to all HTMX requests before they are sent (avoid keyboard context resets during polling updates)
document.body.addEventListener('htmx:beforeRequest', function (event) {
    // Check if the request is originating from the polling container
    if (event.detail.elt.id === 'status-card-container') {
        const activeElement = document.activeElement;

        // If the currently focused element is inside the charging form, cancel the request
        if (activeElement && activeElement.closest('#charging-form')) {
            event.preventDefault();
            // console.debug('HTMX polling paused: Form input is currently focused.'); // Optional, for debugging purposes
        }
    }
});
