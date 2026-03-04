package pb_migrations

import (
	"log"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		// 1. Ensure the embedded server agent exists
		agentsCol, err := app.FindCollectionByNameOrId("agents")
		if err != nil {
			return err
		}
		
		var embeddedAgent *core.Record
		agents, _ := app.FindAllRecords("agents", dbx.HashExp{"fingerprint": "embedded"})
		if len(agents) > 0 {
			embeddedAgent = agents[0]
		} else {
			embeddedAgent = core.NewRecord(agentsCol)
			embeddedAgent.Set("hostname", "Server (Embedded)")
			embeddedAgent.Set("fingerprint", "embedded")
			embeddedAgent.Set("status", "ACTIVE")
			if err := app.Save(embeddedAgent); err != nil {
				return err
			}
			log.Println("[MIGRATE] Created embedded server agent")
		}

		// 2. Add an 'agent' relation to the 'stacks' collection
		stacksCol, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}

		// First, add the field without the Required constraint
		agentField := &core.RelationField{
			Name:         "agent",
			CollectionId: agentsCol.Id,
			MaxSelect:    1,
			Required:     false,
		}
		stacksCol.Fields.Add(agentField)

		if err := app.Save(stacksCol); err != nil {
			return err
		}

		// 3. Assign existing stacks to the embedded agent
		allStacks, err := app.FindAllRecords("stacks")
		if err != nil {
			return err
		}
		for _, stack := range allStacks {
			if stack.GetString("agent") == "" {
				stack.Set("agent", embeddedAgent.Id)
				if err := app.Save(stack); err != nil {
					log.Printf("[MIGRATE] Error assigning agent to stack %s: %v", stack.Id, err)
				}
			}
		}

		return nil

	}, func(app core.App) error {
		stacksCol, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}

		field := stacksCol.Fields.GetByName("agent")
		if field != nil {
			stacksCol.Fields.RemoveByName("agent")
			return app.Save(stacksCol)
		}
		return nil
	})
}
