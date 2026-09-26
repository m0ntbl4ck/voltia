package llm

import (
	"encoding/json"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// PromptVersion changes whenever the prompt or the schema do, so text cached
// under an older wording is not served as if the new one had written it.
const PromptVersion = "4"

// SystemPrompt tells the model its role and the limits of what it may say.
const SystemPrompt = `Eres el analista de mantenimiento de una planta industrial. Recibes el paquete de evidencia de una anomalía en un medidor eléctrico y escribes la explicación que leerá un operador de mantenimiento.

Reglas:
- Escribe en español de Colombia, en un tono directo y sin adornos.
- Usa únicamente los datos del paquete. No inventes causas, equipos ni eventos que no estén en él.
- El motor analítico ya decidió el tipo, la severidad y la confianza. No los cuestiones ni los cambies.
- Cada número que escribas debe estar en el paquete tal cual, redondeado si quieres. No calcules cifras nuevas: nada de sumas, restas, promedios ni proporciones propias.
- Las fechas y horas del paquete ya están en la hora de la planta: no las conviertas. Escríbelas en lenguaje natural, por ejemplo «el 12 de septiembre a las 14:00». No copies el formato del paquete (2026-09-12T14:00:00-05:00) ni escribas el año.
- Escribe los números como en es-CO: punto para los miles y coma para los decimales.
- No menciones cifras que valgan cero, como un exceso de 0 kWh: si no aporta, no lo cuentes.
- Habla como habla un operador: no escribas nombres internos como DATA_QUALITY, HIGH o isolated_electrical_readings. Di «calidad de datos», «severidad alta», «lecturas eléctricas aisladas».
- La acción recomendada sigue el tipo: REAL_ANOMALY, crear una orden de inspección; DATA_QUALITY, solicitar la validación del medidor; EXPLAINABLE_ANOMALY, confirmar con producción que el cambio es el esperado; FALSE_POSITIVE, descartarla. No propongas reparaciones ni nombres de equipos que no estén en el paquete, como cableado o transformadores.
- "summary": una frase con lo que pasó y dónde. "reason": por qué el motor lo clasificó así, con la evidencia. "recommended_action": la acción concreta para el operador. "investigation_steps": de 2 a 5 pasos cortos, en orden.`

// draft is the JSON a model must answer with.
type draft struct {
	Summary            string   `json:"summary"`
	Reason             string   `json:"reason"`
	RecommendedAction  string   `json:"recommended_action"`
	InvestigationSteps []string `json:"investigation_steps"`
}

// Schema is the JSON Schema of the answer, for providers that can enforce it.
var Schema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"summary":            map[string]any{"type": "string"},
		"reason":             map[string]any{"type": "string"},
		"recommended_action": map[string]any{"type": "string"},
		"investigation_steps": map[string]any{
			"type":     "array",
			"items":    map[string]any{"type": "string"},
			"minItems": 2,
			"maxItems": 5,
		},
	},
	"required":             []string{"summary", "reason", "recommended_action", "investigation_steps"},
	"additionalProperties": false,
}

// userPrompt renders the evidence as JSON with every timestamp in loc, so the
// model reads the hours the operator sees and the number guard finds them.
func userPrompt(ev domain.Evidence, loc *time.Location) (string, error) {
	raw, err := json.Marshal(ev)
	if err != nil {
		return "", err
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return "", err
	}
	out, err := json.Marshal(localize(tree, loc))
	if err != nil {
		return "", err
	}
	return "Paquete de evidencia:\n" + string(out), nil
}

func localize(node any, loc *time.Location) any {
	switch v := node.(type) {
	case string:
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t.In(loc).Format(time.RFC3339)
		}
	case []any:
		for i, x := range v {
			v[i] = localize(x, loc)
		}
	case map[string]any:
		for k, x := range v {
			v[k] = localize(x, loc)
		}
	}
	return node
}
