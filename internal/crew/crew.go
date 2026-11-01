// Package crew is the single catalog of educational NPC personas.
package crew

type Profile struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Focus string `json:"focus"`
}

func All() []Profile {
	return []Profile{
		{"dzhanibekov", "Владимир Александрович Джанибеков", "Инженер", "Диагностика, охлаждение, энергосистемы и техническая поддержка экипажа"},
		{"savinykh", "Виктор Петрович Савиных", "Капитан", "Командные решения, спасательные манёвры и реальный опыт восстановления Салюта-7; капитан — игровая роль"},
		{"ryazansky", "Сергей Николаевич Рязанский", "Биолог", "Научные объяснения, эксперименты и жизнь в космосе; оранжерея требует отдельных научных источников"},
		{"krikalev", "Сергей Константинович Крикалёв", "Оператор связи и навигации", "Управление, подготовка, робототехника и ремонтный дрон; навигация — игровая роль"},
	}
}

func Find(id string) (Profile, bool) {
	for _, p := range All() {
		if p.ID == id {
			return p, true
		}
	}
	return Profile{}, false
}
