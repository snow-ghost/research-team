package incident

import "sort"

func BuildReport(input IncidentInput, evidence []Evidence, hypotheses []Hypothesis, eventLog []EventRecord) Report {
	supported, weakened, rejected := splitHypotheses(hypotheses)
	primary := choosePrimaryTeam(hypotheses, evidence)
	secondary := chooseSecondaryTeams(primary, hypotheses)
	report := Report{
		IncidentID:            input.ID,
		Summary:               input.Summary,
		PrimaryTeam:           primary,
		SecondaryTeams:        secondary,
		PrimaryCause:          primaryCause(primary, evidence),
		Confidence:            confidence(hypotheses),
		SupportedHypotheses:   supported,
		WeakenedHypotheses:    weakened,
		RejectedHypotheses:    rejected,
		Findings:              buildFindings(evidence),
		RecommendedActions:    recommendedActions(primary, evidence),
		NotRecommendedActions: notRecommendedActions(primary, evidence),
		EventLog:              append([]EventRecord(nil), eventLog...),
	}
	return report
}

func splitHypotheses(hypotheses []Hypothesis) (supported, weakened, rejected []Hypothesis) {
	for _, hypothesis := range hypotheses {
		switch hypothesis.Status {
		case HypothesisSupported:
			supported = append(supported, hypothesis)
		case HypothesisWeakened:
			weakened = append(weakened, hypothesis)
		case HypothesisRejected:
			rejected = append(rejected, hypothesis)
		}
	}
	return supported, weakened, rejected
}

func choosePrimaryTeam(hypotheses []Hypothesis, evidence []Evidence) Team {
	etcdSupported := isSupported(hypotheses, "H3")
	storageSupported := isSupported(hypotheses, "H6")
	if etcdSupported && storageSupported && (hasSignal(evidence, SignalDiskIOError) || hasSignal(evidence, SignalReadOnlyFilesystem) || hasSignal(evidence, SignalEtcdFsyncLatency)) {
		return TeamStorage
	}
	best := Hypothesis{Score: -1, Owner: TeamUnknown}
	for _, hypothesis := range hypotheses {
		if hypothesis.Status != HypothesisSupported {
			continue
		}
		if hypothesis.Score > best.Score {
			best = hypothesis
		}
	}
	if best.Owner != "" {
		return best.Owner
	}
	return TeamUnknown
}

func chooseSecondaryTeams(primary Team, hypotheses []Hypothesis) []Team {
	seen := map[Team]struct{}{primary: {}}
	var teams []Team
	for _, hypothesis := range hypotheses {
		if hypothesis.Status != HypothesisSupported && hypothesis.Status != HypothesisWeakened {
			continue
		}
		if hypothesis.Owner == TeamUnknown || hypothesis.Owner == "" {
			continue
		}
		if _, ok := seen[hypothesis.Owner]; ok {
			continue
		}
		seen[hypothesis.Owner] = struct{}{}
		teams = append(teams, hypothesis.Owner)
	}
	sort.Slice(teams, func(i, j int) bool { return teams[i] < teams[j] })
	return teams
}

func isSupported(hypotheses []Hypothesis, id string) bool {
	for _, hypothesis := range hypotheses {
		if hypothesis.ID == id {
			return hypothesis.Status == HypothesisSupported
		}
	}
	return false
}

func confidence(hypotheses []Hypothesis) string {
	if len(hypotheses) == 0 || hypotheses[0].Score == 0 {
		return "низкая"
	}
	if hypotheses[0].Score >= 8 {
		return "высокая"
	}
	return "средняя"
}

func primaryCause(team Team, evidence []Evidence) string {
	switch team {
	case TeamStorage:
		if hasSignal(evidence, SignalAPIServerEtcdTimeout) || hasSignal(evidence, SignalEtcdFsyncLatency) {
			return "Вероятна деградация etcd из-за признаков хранилища: ошибок ввода-вывода, режима только чтения, заполнения диска или задержки fsync."
		}
		return "Вероятна проблема хранилища: том, диск, файловая система или CSI."
	case TeamNetwork:
		if hasSignal(evidence, SignalDNSLookupTimeout) || hasSignal(evidence, SignalCoreDNSCrash) {
			return "Вероятна проблема сетевого слоя или DNS, не объясненная более ранним отказом etcd, узла или среды запуска."
		}
		return "Вероятна проблема сетевой связности, CNI, kube-proxy, маршрутов или правил служб."
	case TeamCompute:
		return "Вероятна проблема вычислительного узла, kubelet или ресурсов узла."
	case TeamRuntime:
		return "Вероятна проблема среды запуска контейнеров: containerd, CRI-O, pod sandbox или загрузка образов."
	default:
		return "Недостаточно признаков для выбора команды-владельца."
	}
}

func buildFindings(evidence []Evidence) []Finding {
	groups := map[Category][]Evidence{}
	for _, item := range evidence {
		groups[item.Category] = append(groups[item.Category], item)
	}
	categories := make([]Category, 0, len(groups))
	for category := range groups {
		categories = append(categories, category)
	}
	sort.Slice(categories, func(i, j int) bool { return categories[i] < categories[j] })
	findings := make([]Finding, 0, len(categories))
	for _, category := range categories {
		items := groups[category]
		ids := make([]string, 0, len(items))
		for _, item := range items {
			ids = append(ids, item.ID)
		}
		findings = append(findings, Finding{
			Title:    string(category),
			Details:  "Найдены признаки категории " + string(category) + ".",
			Evidence: ids,
		})
	}
	return findings
}

func recommendedActions(team Team, evidence []Evidence) []string {
	switch team {
	case TeamStorage:
		actions := []string{
			"Проверить кворум etcd и наличие актуальной резервной копии.",
			"Проверить состояние устройства, файловой системы и тома на узле с признаками ошибки.",
			"Если кворум сохранен, восстановить участника etcd на исправном хранилище по регламенту.",
			"После восстановления etcd проверить kube-apiserver, CoreDNS и приложения.",
		}
		if !hasSignal(evidence, SignalAPIServerEtcdTimeout) {
			actions = append(actions, "Проверить CSI, подключение томов и журналы узла.")
		}
		return actions
	case TeamNetwork:
		return []string{
			"Проверить pod-to-pod, pod-to-service и node-to-node связность.",
			"Проверить CNI, kube-proxy или заменяющий компонент, маршруты и сетевые правила.",
			"Для DNS сравнить запрос имени службы и прямой запрос ClusterIP.",
		}
	case TeamCompute:
		return []string{
			"Проверить состояние узла, kubelet, память, процессор и системный диск.",
			"Сопоставить NodeNotReady, MemoryPressure, DiskPressure и журналы ядра.",
		}
	case TeamRuntime:
		return []string{
			"Проверить containerd или CRI-O, crictl info и журналы kubelet.",
			"Проверить ошибки создания pod sandbox и загрузки образов.",
		}
	default:
		return []string{"Собрать дополнительные журналы и повторить оценку гипотез."}
	}
}

func notRecommendedActions(team Team, evidence []Evidence) []string {
	if team == TeamStorage && (hasSignal(evidence, SignalDNSLookupTimeout) || hasSignal(evidence, SignalCoreDNSTimeoutAPI)) {
		return []string{
			"Не начинать с перезапуска CoreDNS, если DNS-ошибки появились после признаков etcd.",
			"Не менять сетевые правила без подтверждения сетевой гипотезы.",
			"Не удалять данные etcd без процедуры восстановления.",
		}
	}
	return []string{"Не выполнять исправления с изменением состояния без подтверждения первичной гипотезы."}
}
