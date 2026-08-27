package command

func applySchemas(definitions []Descriptor) {
	collection := []Property{
		prop("limit", minimum(integer(), 1)), prop("cursor", nullable(stringSchema())),
		prop("all", defaultBoolean(false)), prop("max_items", nullable(minimum(integer(), 1))),
	}
	dryRun := prop("dry_run", defaultBoolean(false))
	eventID := prop("event_id", stringSchema())
	contactRef := prop("contact_ref", stringSchema())
	contactName := prop("contact", stringSchema())

	authResult := object([]string{"authenticated", "token_state", "expires_at"},
		prop("authenticated", boolean()),
		prop("token_state", enum("healthy", "expiring", "expired", "missing")),
		prop("expires_at", nullable(stringSchema())),
	)
	eventSummary := object([]string{"event_id", "title", "start", "end", "timezone", "state", "user_role", "my_rsvp"},
		eventID, prop("title", nullable(stringSchema())), prop("start", nullable(stringSchema())),
		prop("end", nullable(stringSchema())), prop("timezone", nullable(stringSchema())),
		prop("state", nullable(stringSchema())), prop("user_role", nullable(stringSchema())),
		prop("my_rsvp", nullable(eventReadRSVP())),
	)
	poster := object([]string{"poster_id", "name", "url", "content_type", "width", "height", "tags", "categories"},
		prop("poster_id", stringSchema()), prop("name", stringSchema()), prop("url", stringSchema()),
		prop("content_type", stringSchema()), prop("width", integer()), prop("height", integer()),
		prop("tags", array(stringSchema())), prop("categories", array(stringSchema())),
	)
	link := object([]string{"label", "url"}, prop("label", stringSchema()), prop("url", stringSchema()))
	eventDetailProperties := append([]Property(nil), eventSummary.Properties...)
	eventDetailProperties = append(eventDetailProperties,
		prop("description", nullable(stringSchema())), prop("location", nullable(stringSchema())),
		prop("address", nullable(stringSchema())), prop("visibility", nullable(enum("private", "public"))),
		prop("guest_limit", nullable(integer())), prop("poster", nullable(poster)),
		prop("links", nullable(array(link))),
	)
	eventDetail := object(propertyNames(eventDetailProperties), eventDetailProperties...)
	guest := object([]string{"display_name", "rsvp_status", "party_size", "cohost"},
		prop("display_name", stringSchema()), prop("rsvp_status", nullable(eventReadRSVP())),
		prop("party_size", nullable(integer())), prop("cohost", boolean()),
	)
	contact := object([]string{"contact_ref", "display_name", "shared_event_count"},
		contactRef, prop("display_name", stringSchema()), prop("shared_event_count", integer()),
	)
	cohost := object([]string{"display_name", "status"},
		prop("display_name", stringSchema()), prop("status", enum("invited", "revoked", "removed")),
	)
	cohostLink := object([]string{"url", "state"},
		prop("url", nullable(stringSchema())), prop("state", enum("active", "revoked")),
	)

	definitions[0].InputSchema = emptyObject()
	definitions[0].ResultSchema = authResult
	definitions[1].InputSchema = emptyObject()
	definitions[1].ResultSchema = authResult
	definitions[2].InputSchema = object(nil, dryRun)
	definitions[2].ResultSchema = authResult

	definitions[3].InputSchema = object([]string{"when"}, append([]Property{prop("when", enum("upcoming", "past"))}, collection...)...)
	definitions[3].ResultSchema = collectionResult("events", eventSummary)
	definitions[4].InputSchema = object([]string{"event_id"}, eventID)
	definitions[4].ResultSchema = object([]string{"event"}, prop("event", eventDetail))

	createProperties := []Property{
		prop("title", stringSchema()), prop("start", formatted(stringSchema(), "date-time")), prop("timezone", formatted(stringSchema(), "iana-timezone")),
		prop("end", nullable(formatted(stringSchema(), "date-time"))), prop("description", nullable(stringSchema())),
		prop("location", nullable(stringSchema())), prop("visibility", nullable(enum("private", "public"))),
		prop("guest_limit", nullable(integer())), prop("links", array(link)),
		prop("poster_id", nullable(stringSchema())), dryRun,
	}
	definitions[5].InputSchema = object([]string{"title", "start", "timezone"}, createProperties...)
	definitions[5].ResultSchema = object([]string{"submitted"}, prop("submitted", boolean()))

	updateProperties := []Property{
		eventID, prop("title", stringSchema()), prop("description", nullable(stringSchema())),
		prop("start", formatted(stringSchema(), "date-time")), prop("end", nullable(formatted(stringSchema(), "date-time"))), prop("timezone", formatted(stringSchema(), "iana-timezone")),
		prop("guest_limit", nullable(integer())), prop("links", nullable(array(link))),
		prop("poster_id", nullable(stringSchema())), dryRun,
	}
	definitions[6].InputSchema = object([]string{"event_id"}, updateProperties...)
	definitions[6].ResultSchema = object([]string{"event_id", "fields", "submitted"},
		eventID, prop("fields", array(stringSchema())), prop("submitted", boolean()))
	definitions[7].InputSchema = object([]string{"event_id"},
		eventID, prop("message", nullable(stringSchema())), prop("notify_guests", defaultBoolean(true)), dryRun)
	definitions[7].ResultSchema = object([]string{"event_id", "notify_guests", "submitted"},
		eventID, prop("notify_guests", boolean()), prop("submitted", boolean()))

	definitions[8].InputSchema = object([]string{"event_id"}, append([]Property{eventID}, collection...)...)
	definitions[8].ResultSchema = collectionResult("guests", guest)
	definitions[9].InputSchema = contactSelectorObject([]Property{eventID, contactRef, contactName, prop("message", nullable(stringSchema())), dryRun}, []string{"event_id"})
	definitions[9].ResultSchema = object([]string{"event_id", "submitted"}, eventID, prop("submitted", boolean()))
	definitions[10].InputSchema = object([]string{"event_id"}, eventID)
	definitions[10].ResultSchema = object([]string{"rsvp"}, prop("rsvp", object([]string{"event_id", "status"}, eventID, prop("status", nullable(eventReadRSVP())))))

	rsvpCommon := []Property{
		eventID, prop("status", enum("going", "not-going", "interested", "not-interested")),
		prop("display_name", stringSchema()), prop("party_size", minimum(integer(), 1)), prop("plus_ones", array(stringSchema())),
		prop("timezone", formatted(stringSchema(), "iana-timezone")), prop("message", nullable(stringSchema())),
		prop("questionnaire_response", nullable(object(nil))), dryRun,
	}
	definitions[11].InputSchema = Schema{Kind: "object", Discriminator: "status", OneOf: []Schema{
		object([]string{"event_id", "status", "display_name", "party_size", "timezone"}, withStatus(rsvpCommon, "going")...),
		object([]string{"event_id", "status", "display_name", "party_size", "timezone"}, withStatus(rsvpCommon, "not-going")...),
		object([]string{"event_id", "status"}, withStatus([]Property{eventID, prop("status", enum("interested")), dryRun}, "interested")...),
		object([]string{"event_id", "status"}, withStatus([]Property{eventID, prop("status", enum("not-interested")), dryRun}, "not-interested")...),
	}}
	definitions[11].validateInput = validateRSVPInput
	definitions[11].ResultSchema = object([]string{"event_id", "intent", "submitted"}, eventID, prop("intent", enum("going", "not-going", "interested", "not-interested")), prop("submitted", boolean()))

	definitions[12].InputSchema = object(nil, append([]Property{prop("query", nullable(stringSchema()))}, collection...)...)
	definitions[12].ResultSchema = collectionResult("contacts", contact)
	for index := 13; index <= 15; index++ {
		definitions[index].InputSchema = contactSelectorObject([]Property{eventID, contactRef, contactName, dryRun}, []string{"event_id"})
		definitions[index].ResultSchema = object([]string{"event_id", "cohost"}, eventID, prop("cohost", cohost))
	}
	for index := 16; index <= 17; index++ {
		definitions[index].InputSchema = object([]string{"event_id"}, eventID, dryRun)
		definitions[index].ResultSchema = object([]string{"event_id", "link"}, eventID, prop("link", cohostLink))
	}
	definitions[18].InputSchema = object([]string{"event_id", "audience", "message"},
		eventID, prop("audience", enum("all-guests")), prop("message", stringSchema()),
		prop("show_on_event_page", defaultBoolean(false)), dryRun)
	definitions[18].ResultSchema = object([]string{"event_id", "submitted", "audience", "show_on_event_page", "recipient_status"},
		eventID, prop("submitted", boolean()), prop("audience", enum("all-guests")),
		prop("show_on_event_page", boolean()), prop("recipient_status", enum("not-reported")))

	definitions[19].InputSchema = object(nil, collection...)
	definitions[19].ResultSchema = collectionResult("posters", poster)
	definitions[20].InputSchema = object([]string{"query"}, append([]Property{prop("query", stringSchema())}, collection...)...)
	definitions[20].ResultSchema = collectionResult("posters", poster)
	definitions[21].InputSchema = object(nil, prop("command", nullable(stringSchema())))
	definitions[21].ResultSchema = commandSchemaResultSchema()
	definitions[22].InputSchema = emptyObject()
	definitions[22].ResultSchema = object([]string{"healthy", "checks"},
		prop("healthy", boolean()), prop("checks", array(object([]string{"name", "status", "message", "remediation"},
			prop("name", stringSchema()), prop("status", enum("pass", "warn", "fail")),
			prop("message", stringSchema()), prop("remediation", nullable(stringSchema()))))))
	definitions[23].InputSchema = emptyObject()
	definitions[23].ResultSchema = object([]string{"cli_version", "command_contract_revision", "transport_contract_revision"},
		prop("cli_version", stringSchema()), prop("command_contract_revision", stringSchema()),
		prop("transport_contract_revision", stringSchema()))
}

func commandSchemaResultSchema() Schema {
	hints := object([]string{"read_only", "destructive", "idempotent", "open_world"},
		prop("read_only", boolean()), prop("destructive", boolean()), prop("idempotent", boolean()), prop("open_world", boolean()))
	mcp := object([]string{"name", "hints"}, prop("name", stringSchema()), prop("hints", hints))
	command := object([]string{"id", "cli_path", "help", "input_schema", "result_schema", "failure_types", "authorization", "risk", "dry_run"},
		prop("id", stringSchema()), prop("cli_path", stringSchema()), prop("help", stringSchema()),
		prop("positionals", array(object(nil))), prop("flags", array(object(nil))),
		prop("input_schema", object(nil)), prop("result_schema", object(nil)),
		prop("failure_types", array(stringSchema())), prop("authorization", stringSchema()),
		prop("risk", stringSchema()), prop("dry_run", boolean()), prop("mcp", nullable(mcp)))
	return object([]string{"commands", "mcp_tools", "cli_only_commands"},
		prop("commands", array(command)), prop("mcp_tools", array(stringSchema())), prop("cli_only_commands", array(stringSchema())))
}

func contactSelectorObject(properties []Property, required []string) Schema {
	byRef := replaceProperties(properties, "contact", nil)
	byName := replaceProperties(properties, "contact_ref", nil)
	return Schema{Kind: "object", Discriminator: "contact_selector", OneOf: []Schema{
		object(append(append([]string(nil), required...), "contact_ref"), byRef...),
		object(append(append([]string(nil), required...), "contact"), byName...),
	}}
}

func replaceProperties(properties []Property, remove string, replacement *Property) []Property {
	result := make([]Property, 0, len(properties))
	for _, property := range properties {
		if property.Name != remove {
			result = append(result, property)
		}
	}
	if replacement != nil {
		result = append(result, *replacement)
	}
	return result
}

func withStatus(properties []Property, status string) []Property {
	result := append([]Property(nil), properties...)
	for index := range result {
		if result[index].Name == "status" {
			result[index].Schema = enum(status)
		}
	}
	return result
}

func collectionResult(name string, item Schema) Schema {
	return object([]string{name, "next_cursor", "has_more"},
		prop(name, array(item)), prop("next_cursor", nullable(stringSchema())), prop("has_more", boolean()))
}

func eventReadRSVP() Schema {
	return enum("ready-to-send", "sending", "send-error", "delivery-error", "sent", "interested", "waitlist", "maybe", "declined", "going", "pending-approval", "approved", "withdrawn", "waitlisted-for-approval", "rejected", "responded-to-find-a-time")
}

func emptyObject() Schema { return object(nil) }
func object(required []string, properties ...Property) Schema {
	return Schema{Kind: "object", Required: append([]string(nil), required...), Properties: properties}
}
func prop(name string, schema Schema) Property      { return Property{Name: name, Schema: schema} }
func stringSchema() Schema                          { return Schema{Kind: "string"} }
func integer() Schema                               { return Schema{Kind: "integer"} }
func boolean() Schema                               { return Schema{Kind: "boolean"} }
func enum(values ...string) Schema                  { return Schema{Kind: "string", Enum: values} }
func array(items Schema) Schema                     { return Schema{Kind: "array", Items: &items} }
func nullable(schema Schema) Schema                 { schema.Nullable = true; return schema }
func formatted(schema Schema, format string) Schema { schema.Format = format; return schema }
func defaultBoolean(value bool) Schema              { return Schema{Kind: "boolean", DefaultBool: &value} }
func minimum(schema Schema, value int) Schema       { schema.Minimum = &value; return schema }
func propertyNames(properties []Property) []string {
	result := make([]string, len(properties))
	for index, property := range properties {
		result[index] = property.Name
	}
	return result
}
