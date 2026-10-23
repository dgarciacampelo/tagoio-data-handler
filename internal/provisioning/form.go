// This file holds the CPO sign-up form. The notification service sends it with snake_case keys; the Google Form
// webhook used the question titles in Spanish. Both are accepted, and the form is echoed back with the Spanish
// keys, as the Python service did.

package provisioning

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strings"
)

// Form is a CPO sign-up form.
type Form struct {
	UserEmail      string // Login of the new TagoIO user
	CompanyName    string
	FiscalID       string
	CompanyAddress string
	ContactPhone   string
	ContactEmail   string
	ManagerName    string
	ManagerDNI     string
	IBAN           string
	PaymentSystem  string // Contains "TPV" when the stations get payment terminals
	Description    string
	Timestamp      string // Optional
}

// formField maps a form field to its snake_case key and its Spanish question title.
type formField struct {
	key      string
	alias    string
	required bool
	email    bool
	value    func(*Form) *string
}

var formFields = []formField{
	{"user_email", "Dirección de correo electrónico", true, true, func(f *Form) *string { return &f.UserEmail }},
	{"company_name", "Nombre de la Empresa", true, false, func(f *Form) *string { return &f.CompanyName }},
	{"cpo_fiscal_id", "CIF de la Empresa", true, false, func(f *Form) *string { return &f.FiscalID }},
	{"company_address", "Dirección de la Empresa", true, false, func(f *Form) *string { return &f.CompanyAddress }},
	{"contact_phone", "Teléfono de contacto", true, false, func(f *Form) *string { return &f.ContactPhone }},
	{"contact_email", "Correo electrónico de contacto", true, true, func(f *Form) *string { return &f.ContactEmail }},
	{"manager_name", "Nombre del Apoderado de la Empresa", true, false, func(f *Form) *string { return &f.ManagerName }},
	{"manager_dni", "DNI del Apoderado de la Empresa", true, false, func(f *Form) *string { return &f.ManagerDNI }},
	{"iban", "IBAN Bancario", true, false, func(f *Form) *string { return &f.IBAN }},
	{"payment_system", "Añadir sistema de Pago en los Puntos de Recarga", true, false, func(f *Form) *string { return &f.PaymentSystem }},
	{"description", "Descripción del sitio para la instalación.", true, false, func(f *Form) *string { return &f.Description }},
	{"timestamp", "Marca temporal", false, false, func(f *Form) *string { return &f.Timestamp }},
}

// UnmarshalJSON reads the form with either key style and validates it.
func (f *Form) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var errs []error
	for _, field := range formFields {
		value, ok := raw[field.key]
		if !ok {
			value, ok = raw[field.alias]
		}
		text, isText := value.(string)
		if ok && value != nil && !isText {
			errs = append(errs, fmt.Errorf("%s must be a string", field.key))
			continue
		}
		text = strings.TrimSpace(text)
		if field.required && text == "" {
			errs = append(errs, fmt.Errorf("%s is required", field.key))
			continue
		}
		if field.email && text != "" {
			if _, err := mail.ParseAddress(text); err != nil {
				errs = append(errs, fmt.Errorf("%s is not a valid email address", field.key))
				continue
			}
		}
		*field.value(f) = text
	}
	return errors.Join(errs...)
}

// MarshalJSON writes the form with the Spanish question titles as keys.
func (f Form) MarshalJSON() ([]byte, error) {
	out := make(map[string]string, len(formFields))
	for _, field := range formFields {
		out[field.alias] = *field.value(&f)
	}
	return json.Marshal(out)
}
