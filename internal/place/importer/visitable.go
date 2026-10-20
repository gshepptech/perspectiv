package importer

import (
	"strings"
	"unicode"
)

// A place is worth listing when someone could have been there and have a
// story about it. The source datasets also carry offices and services that
// nobody visits as a place (a realtor, a plumber, an ATM, a B2B supplier),
// and places a story could expose someone at (a clinic, a therapist, a home,
// a jail). Those are left out at import and counted in Plan.Unlisted. A
// record with no category is kept: a missing category says nothing.

// overtureUnlisted are Overture basic_category values left out.
var overtureUnlisted = map[string]bool{
	// Offices and services, not places people go.
	"real_estate_service": true, "home_service": true, "financial_service": true,
	"professional_service": true, "attorney_or_law_firm": true, "legal_service": true,
	"building_or_construction_service": true, "atm": true, "technical_service": true,
	"media_service": true, "design_service": true, "corporate_or_business_office": true,
	"shipping_or_delivery_service": true, "storage_facility": true, "supplier_or_distributor": true,
	"b2b_service": true, "manufacturer": true, "printing_service": true,
	"b2b_office_and_professional_service": true, "b2b_transportation_and_storage_service": true,
	"wholesaler": true, "b2b_industrial_and_machine_service": true, "b2b_energy_and_utility_service": true,
	"b2b_science_and_technology_service": true, "telecommunications_service": true,
	"security_service": true, "industrial_facility_or_service": true, "agricultural_service": true,
	"housing_or_property_service": true, "event_or_party_service": true, "rental_service": true,
	"family_service": true, "environmental_or_ecological_service": true, "food_service": true,
	"automotive_service": true, "vehicle_service": true, "taxi_or_ride_share_service": true,
	"air_transport_facility_or_service": true, "ev_charging_station": true, "parking": true,
	"parking_lot": true, "parking_garage": true, "toll_station": true, "travel_and_transportation": true,
	"political_organization": true, "labor_union": true, "public_utility": true,
	"water_utility_provider": true, "electric_utility_provider": true,
	// Homes, and care where a story could expose a patient.
	"apartment": true, "condominium": true, "senior_living_facility": true, "jail_or_prison": true,
	"health_care": true, "specialized_health_care": true, "dental_clinic": true,
	"diagnostics_imaging_or_lab_service": true, "complementary_and_alternative_medicine": true,
	"behavioral_or_mental_health_clinic": true, "medical_service": true,
	"primary_care_or_general_clinic": true, "physical_medicine_and_rehabilitation": true,
	"outpatient_care_facility": true, "vision_or_eye_care_clinic": true, "surgery": true,
	"specialized_medical_facility": true, "reproductive_perinatal_and_womens_care": true,
	"pediatric_clinic": true,
}

// fsqUnlistedTop are the first segments of a Foursquare category label left
// out, and fsqListedAnyway the last segments that keep a place in them.
var (
	fsqUnlistedTop  = map[string]bool{"business and professional services": true, "health and medicine": true}
	fsqListedAnyway = map[string]bool{"laundromat": true, "hospital": true, "emergency room": true, "bank": true}
)

// overtureKeptIn are the only categories kept inside the top-level groups
// that are otherwise left out whole: in services_and_business only the
// laundromat and the bank are places people go and talk about, and in
// health_care only the hospital and the emergency room are public enough.
var overtureKeptIn = map[string]map[string]bool{
	"services_and_business": {"laundry_service": true, "laundromat": true, "bank_or_credit_union": true},
	"health_care": {
		"hospital": true, "emergency_or_urgent_care_facility": true, "urgent_care_clinic": true,
		"walk_in_clinic": true,
	},
}

// overtureListed applies the top-level group rule first, where the record
// has a taxonomy, and the per-category list after it. A group rule catches
// the long tail of sub-categories (junk removal, sign printing) a list of
// names would miss.
func overtureListed(p *placeRecord) bool {
	cat := p.BasicCategory
	if cat == "" && p.Categories != nil {
		cat = p.Categories.Primary
	}
	if p.Taxonomy != nil && len(p.Taxonomy.Hierarchy) > 0 {
		if kept, grouped := overtureKeptIn[p.Taxonomy.Hierarchy[0]]; grouped {
			if kept[cat] {
				return true
			}
			for _, h := range p.Taxonomy.Hierarchy[1:] {
				if kept[h] {
					return true
				}
			}
			return false
		}
	}
	return !overtureUnlisted[cat]
}

func fsqListed(labels []string) bool {
	for _, l := range labels {
		parts := strings.Split(l, ">")
		top, last := nameKey(parts[0]), nameKey(parts[len(parts)-1])
		if fsqUnlistedTop[top] && !fsqListedAnyway[last] {
			return false
		}
	}
	return true
}

// hasLetter is false for a name of only digits and symbols ("." or "007"),
// which names no place a reader could recognise.
func hasLetter(name string) bool {
	for _, r := range name {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}
