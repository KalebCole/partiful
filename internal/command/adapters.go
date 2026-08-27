package command

func applyAdapters(definitions []Descriptor) {
	collection := []Flag{
		flag("limit", "limit"), flag("cursor", "cursor"), flag("all", "all"), flag("max-items", "max_items"),
	}
	dryRun := flag("dry-run", "dry_run")
	eventID := Positional{Name: "event-id", Field: "event_id", Required: true}
	contact := []Flag{flag("contact-ref", "contact_ref"), flag("contact", "contact")}

	definitions[2].CLI.Flags = []Flag{dryRun}
	definitions[3].CLI.Flags = append([]Flag{requiredFlag("when", "when")}, collection...)
	definitions[4].CLI.Positionals = []Positional{eventID}
	definitions[5].CLI.Flags = []Flag{
		requiredFlag("title", "title"), requiredFlag("start", "start"), requiredFlag("timezone", "timezone"),
		flag("end", "end"), flag("description", "description"), flag("location", "location"),
		flag("visibility", "visibility"), flag("guest-limit", "guest_limit"), repeatedFlag("link", "links"),
		flag("poster-id", "poster_id"), dryRun,
	}
	definitions[6].CLI.Positionals = []Positional{eventID}
	definitions[6].CLI.Flags = []Flag{
		flag("title", "title"), flag("description", "description"), flag("start", "start"),
		flag("end", "end"), flag("timezone", "timezone"), flag("guest-limit", "guest_limit"),
		repeatedFlag("link", "links"), flag("poster-id", "poster_id"),
		flag("clear-description", "description"), flag("clear-end", "end"),
		flag("clear-guest-limit", "guest_limit"), flag("clear-links", "links"),
		flag("clear-poster", "poster_id"), dryRun,
	}
	definitions[7].CLI.Positionals = []Positional{eventID}
	definitions[7].CLI.Flags = []Flag{flag("message", "message"), flag("notify-guests", "notify_guests"), dryRun}
	definitions[8].CLI.Positionals = []Positional{eventID}
	definitions[8].CLI.Flags = append([]Flag(nil), collection...)
	definitions[9].CLI.Positionals = []Positional{eventID}
	definitions[9].CLI.Flags = append(append([]Flag(nil), contact...), contentFlag("message-file", "message"), dryRun)
	definitions[10].CLI.Positionals = []Positional{eventID}
	definitions[11].CLI.Positionals = []Positional{eventID}
	definitions[11].CLI.Flags = []Flag{
		requiredFlag("status", "status"), flag("display-name", "display_name"), flag("party-size", "party_size"),
		repeatedFlag("plus-one", "plus_ones"), flag("timezone", "timezone"),
		contentFlag("message-file", "message"), contentFlag("questionnaire-response-file", "questionnaire_response"), dryRun,
	}
	definitions[12].CLI.Flags = append([]Flag{flag("query", "query")}, collection...)
	for index := 13; index <= 15; index++ {
		definitions[index].CLI.Positionals = []Positional{eventID}
		definitions[index].CLI.Flags = append(append([]Flag(nil), contact...), dryRun)
	}
	for index := 16; index <= 17; index++ {
		definitions[index].CLI.Positionals = []Positional{eventID}
		definitions[index].CLI.Flags = []Flag{dryRun}
	}
	definitions[18].CLI.Positionals = []Positional{eventID}
	definitions[18].CLI.Flags = []Flag{
		requiredFlag("audience", "audience"), requiredContentFlag("message-file", "message"),
		flag("show-on-event-page", "show_on_event_page"), dryRun,
	}
	definitions[19].CLI.Flags = append([]Flag(nil), collection...)
	definitions[20].CLI.Flags = append([]Flag{requiredFlag("query", "query")}, collection...)
	definitions[21].CLI.Positionals = []Positional{{Name: "command.path", Field: "command"}}
	definitions[23].CLI.Aliases = []string{"--version"}
}

func flag(name, field string) Flag { return Flag{Name: name, Field: field} }
func requiredFlag(name, field string) Flag {
	return Flag{Name: name, Field: field, Required: true}
}
func repeatedFlag(name, field string) Flag {
	return Flag{Name: name, Field: field, Repeated: true}
}
func contentFlag(name, field string) Flag {
	return Flag{Name: name, Field: field, ContentSource: true}
}
func requiredContentFlag(name, field string) Flag {
	return Flag{Name: name, Field: field, Required: true, ContentSource: true}
}
