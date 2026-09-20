package incident

import "fmt"

type Blueprint struct {
	ID          string
	Role        string
	Description string
}

type Factory struct {
	creators map[string]func() Agent
}

func NewFactory() *Factory {
	f := &Factory{creators: map[string]func() Agent{}}
	f.Register(Blueprint{ID: "evidence-extractor", Role: "Сборщик признаков", Description: "Извлекает признаки из журналов."}, func() Agent {
		return EvidenceExtractorAgent{}
	})
	f.Register(Blueprint{ID: "hypothesis-builder", Role: "Генератор гипотез", Description: "Строит и оценивает дерево гипотез."}, func() Agent {
		return HypothesisAgent{}
	})
	f.Register(Blueprint{ID: "incident-router", Role: "Маршрутизатор", Description: "Выбирает команду-владельца и действия."}, func() Agent {
		return RoutingAgent{}
	})
	return f
}

func (f *Factory) Register(blueprint Blueprint, creator func() Agent) {
	if blueprint.ID == "" {
		panic("blueprint id is required")
	}
	if creator == nil {
		panic("agent creator is required")
	}
	f.creators[blueprint.ID] = creator
}

func (f *Factory) Build(id string) (Agent, error) {
	creator, ok := f.creators[id]
	if !ok {
		return nil, fmt.Errorf("unknown agent blueprint: %s", id)
	}
	return creator(), nil
}

func DefaultTeam() []Agent {
	f := NewFactory()
	ids := []string{"evidence-extractor", "hypothesis-builder", "incident-router"}
	agents := make([]Agent, 0, len(ids))
	for _, id := range ids {
		agent, err := f.Build(id)
		if err != nil {
			panic(err)
		}
		agents = append(agents, agent)
	}
	return agents
}
