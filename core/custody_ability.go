package core

// CustodyEndpoint is the narrow typed endpoint of a Place template. Locations
// are literal selectors; Character/Item endpoints use existing argument kinds.
type CustodyEndpoint struct {
	Kind     EntityKind     `json:"kind"`
	Subject  AbilityOperand `json:"subject"`
	Location LocationID     `json:"location,omitempty"`
}
type PlaceTemplate struct {
	Item    AbilityOperand  `json:"item"`
	Handler AbilityOperand  `json:"handler"`
	From    CustodyEndpoint `json:"from"`
	To      CustodyEndpoint `json:"to"`
}

func placeOperands(v PlaceTemplate) ([]templateOperand, error) {
	out := []templateOperand{{"item", v.Item, ItemArgument}, {"handler", v.Handler, CharacterArgument}}
	for _, e := range []struct {
		field    string
		endpoint CustodyEndpoint
	}{{"from", v.From}, {"to", v.To}} {
		r := e.endpoint
		if r.Kind == LocationKind {
			if r.Subject.Parameter != "" || r.Subject.Literal != nil || !validID(string(r.Location)) {
				return nil, abilityFault("E_IR", e.field, "literal Location endpoint", "invalid shape")
			}
			continue
		}
		kind := CharacterArgument
		if r.Kind == ItemKind {
			kind = ItemArgument
		} else if r.Kind != CharacterKind {
			return nil, abilityFault("E_TYPE", e.field, "character, item or location", string(r.Kind))
		}
		if r.Location != "" {
			return nil, abilityFault("E_IR", e.field, "one endpoint variant", "extra location")
		}
		out = append(out, templateOperand{e.field, r.Subject, kind})
	}
	return out, nil
}
func bindCustody(r CustodyEndpoint, args map[ParameterID]AbilityValue) Ref {
	if r.Kind == LocationKind {
		return Ref{LocationKind, string(r.Location)}
	}
	v := operandValue(r.Subject, args)
	if r.Kind == CharacterKind {
		return Ref{CharacterKind, string(*v.Character)}
	}
	return Ref{ItemKind, string(*v.Item)}
}
func clonePlaceTemplate(p *PlaceTemplate) *PlaceTemplate {
	if p == nil {
		return nil
	}
	v := *p
	v.Item, v.Handler = cloneAbilityOperand(p.Item), cloneAbilityOperand(p.Handler)
	v.From.Subject, v.To.Subject = cloneAbilityOperand(p.From.Subject), cloneAbilityOperand(p.To.Subject)
	return &v
}
func (b Blueprint) checkPlaceTemplate(d AbilityDefinition, index int, p PlaceTemplate) error {
	for _, e := range []struct {
		field    string
		endpoint CustodyEndpoint
	}{{"from", p.From}, {"to", p.To}} {
		r := e.endpoint
		if r.Kind == LocationKind || r.Subject.Literal != nil {
			if err := b.CheckCustodyRef(bindCustody(r, nil)); err != nil {
				return abilityError(d.ID, index, e.field, "", err)
			}
		}
	}
	return nil
}
