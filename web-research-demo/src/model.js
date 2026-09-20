export const STORAGE_KEY = "research-team-demo-v1";
export const statuses = {
  open: "Открыто",
  in_review: "На проверке",
  accepted: "Принято",
  refuted: "Опровергнуто",
  challenged: "Оспорено",
  blocked: "Зависимость оспорена",
  needs_changes: "Нужны исправления",
};
export const kinds = {
  goal: "Гипотеза",
  claim: "Подгипотеза",
  lemma: "Лемма",
  obligation: "Обязательство",
  application: "Применение",
};
const clone = (value) => structuredClone(value);

function entity(id, title, kind, study, status, statement, extras = {}) {
  return {
    id,
    title,
    kind,
    study,
    status,
    statement,
    revision: 1,
    author: "agent",
    domain: "Конечные простые неориентированные графы",
    assumptions: "n >= 1",
    dependencies: [],
    proof: "",
    ...extras,
  };
}
const onlyData = (state) => {
  const { history, ...data } = state;
  return data;
};

export function createInitialState() {
  const data = {
    schema: 1,
    revision: 1,
    paused: false,
    scenarioStep: 0,
    studies: [
      {
        id: "forest",
        title: "Число ребер в лесу",
        goal: "H0",
        category: "Теория графов",
        formula: "m = n - c",
      },
      {
        id: "degree",
        title: "Вершина малой степени",
        goal: "H3",
        category: "Теория графов",
        formula: "deg(v) <= 2m / n",
      },
    ],
    entities: [
      entity(
        "H0",
        "Число ребер в лесу",
        "goal",
        "forest",
        "open",
        "В конечном лесу с n вершинами и c компонентами связности число ребер m равно n - c.",
        {
          assumptions: "Граф ацикличен; n >= 1; c >= 1",
          dependencies: ["H1", "H2", "O1"],
          proof:
            "Связный случай дает m_i = n_i - 1. Суммирование по всем компонентам дает m = n - c.",
        },
      ),
      entity(
        "H1",
        "Связный случай",
        "claim",
        "forest",
        "in_review",
        "В конечном дереве с n >= 1 вершинами число ребер равно n - 1.",
        {
          dependencies: ["L1"],
          assumptions: "Граф связен и ацикличен",
          proof:
            "База n = 1: ребер нет. При n > 1 удалить лист и одно инцидентное ребро. Получается дерево на n - 1 вершинах. Индукция дает n - 2 ребра, восстановление добавляет одно.",
        },
      ),
      entity(
        "H2",
        "Сумма по компонентам",
        "claim",
        "forest",
        "open",
        "Сложить принятые оценки компонент и получить m = n - c.",
        {
          dependencies: ["H1"],
          proof:
            "Компоненты не пересекаются по вершинам, между ними нет ребер. Поэтому m = sum(m_i), n = sum(n_i), а sum(1) = c.",
        },
      ),
      entity(
        "O1",
        "Покрытие случаев",
        "obligation",
        "forest",
        "accepted",
        "Каждая компонента конечного леса является деревом, включая изолированную вершину.",
        {
          proof:
            "Компонента связна по определению; цикл в ней был бы циклом исходного графа.",
        },
      ),
      entity(
        "L1",
        "В дереве есть лист",
        "lemma",
        "forest",
        "accepted",
        "Конечное дерево хотя бы с двумя вершинами имеет вершину степени один.",
        {
          assumptions: "Связность, ацикличность, n >= 2",
          proof:
            "Рассмотрим конец максимального простого пути. Дополнительный сосед либо продолжает путь, либо создает цикл. Поэтому степень конца равна одному.",
        },
      ),
      entity(
        "H-bad",
        "Достаточно связности",
        "claim",
        "forest",
        "refuted",
        "У любого связного простого графа m <= n - 1.",
        {
          assumptions: "Только связность; условие ацикличности удалено",
          proof:
            "Контрпример: цикл из трех вершин. В нем n = 3 и m = 3, тогда как n - 1 = 2.",
          counterexample:
            "Треугольник: V = {1, 2, 3}, E = {{1, 2}, {2, 3}, {1, 3}}. Все условия связности выполнены.",
        },
      ),
      entity(
        "L2",
        "Сумма степеней",
        "lemma",
        "forest",
        "accepted",
        "Сумма степеней вершин конечного простого неориентированного графа равна удвоенному числу ребер.",
        {
          assumptions: "Конечность; каждое ребро имеет два разных конца",
          proof:
            "Подсчитать пары «вершина, инцидентное ребро». По вершинам получается сумма степеней, по ребрам получается 2m.",
        },
      ),
      entity(
        "H3",
        "Вершина малой степени",
        "goal",
        "degree",
        "open",
        "В графе с n > 0 вершинами и m ребрами есть вершина степени не больше 2m/n.",
        {
          dependencies: ["O2"],
          assumptions: "n > 0",
          proof:
            "После доказательства формулы средней степени применить усреднение на непустом конечном множестве вершин.",
        },
      ),
      entity(
        "O2",
        "Переход от среднего",
        "obligation",
        "degree",
        "open",
        "Обосновать существование вершины со степенью не больше средней.",
        {
          assumptions: "Непустое конечное множество вершин",
          proof:
            "Если каждое значение строго больше среднего, их сумма тоже строго больше суммы, определяющей среднее: противоречие.",
        },
      ),
    ],
    workLinks: [
      ["H0", "H1"],
      ["H0", "H2"],
      ["H0", "O1"],
      ["H1", "L1"],
      ["H0", "H-bad"],
      ["H3", "O2"],
    ],
    tasks: [
      {
        id: "T1",
        target: "H1",
        title: "Проверить индукционный переход",
        agent: "Проверяющий",
        skill: "R03",
        state: "review",
      },
      {
        id: "T2",
        target: "H2",
        title: "Собрать аргумент для компонент",
        agent: "Исследователь",
        skill: "M08",
        state: "running",
      },
      {
        id: "T3",
        target: "H-bad",
        title: "Проверить условие связности",
        agent: "Поиск опровержения",
        skill: "M09",
        state: "done",
      },
    ],
    questions: [],
    findings: [],
    applications: [],
  };
  return {
    ...data,
    history: [
      {
        id: 1,
        time: "09:40",
        label: "Открыт демонстрационный снимок",
        detail:
          "Два исследования, общая библиотека и один проверенный контрпример.",
        target: "H0",
        actor: "Сценарий",
        snapshot: clone(data),
      },
    ],
  };
}

export function effectiveStatus(state, id, trail = []) {
  const item = state.entities.find((x) => x.id === id);
  if (!item || trail.includes(id)) return "blocked";
  if (item.status === "challenged") return "challenged";
  if (
    item.dependencies.some((dep) =>
      ["challenged", "blocked", "refuted"].includes(
        effectiveStatus(state, dep, [...trail, id]),
      ),
    )
  )
    return "blocked";
  return item.status;
}

export function summarize(state, id) {
  const item = state.entities.find((x) => x.id === id);
  if (!item) return "Объект не найден в этом снимке.";
  const missing = item.dependencies.filter(
    (dep) => effectiveStatus(state, dep) !== "accepted",
  );
  return `По снимку v${state.revision}: ${item.statement} Условия: ${item.assumptions}. Состояние: ${statuses[effectiveStatus(state, id)]}.${missing.length ? " Открытые зависимости: " + missing.join(", ") + "." : " Незакрытых зависимостей в записи нет."}${item.counterexample ? " " + item.counterexample : ""} Основание: ${id}@${item.revision}. Это сведения из данных макета, не ответ действующего агента.`;
}

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

export function transition(state, action) {
  const data = clone(onlyData(state));
  const item = data.entities.find((x) => x.id === action.target);
  const next = state.revision + 1;
  const addTask = (target, title, skill, agent = "Исследователь") =>
    data.tasks.push({
      id: "T-" + next,
      target,
      title,
      skill,
      agent,
      state: "queued",
    });
  let label = "",
    detail = "",
    target = action.target || "H0",
    actor = "Вы";
  if (action.type === "PAUSE") {
    data.paused = !data.paused;
    label = data.paused ? "Поиск приостановлен" : "Поиск продолжен";
    detail =
      "Изменено только исполнение демонстрационного сценария. Математический статус сохранен.";
  } else if (action.type === "QUESTION") {
    assert(item && action.text?.trim(), "Укажите вопрос и выберите объект.");
    assert(action.text.length <= 4000, "Вопрос слишком длинный.");
    data.questions.push({
      id: "Q-" + next,
      target: item.id,
      text: action.text.trim(),
      kind: action.kind || "question",
      snapshot: state.revision,
      answer:
        action.kind === "proposal"
          ? "Предложение сохранено. Задание не создавалось."
          : summarize(state, item.id),
    });
    label =
      action.kind === "proposal" ? "Записано предложение" : "Задан вопрос";
    detail = action.text.trim();
  } else if (action.type === "COUNTEREXAMPLE") {
    assert(item, "Выберите утверждение.");
    addTask(
      item.id,
      "Поиск контрпримера: " + item.title,
      "M09",
      "Поиск опровержения",
    );
    label = "Назначен поиск контрпримера";
    detail = "Создано задание в данных макета. Вычисление не запускалось.";
  } else if (action.type === "SPLIT") {
    assert(
      item && !["accepted", "refuted"].includes(item.status),
      "Принятый или опровергнутый результат нельзя переписать разбиением.",
    );
    assert(
      action.parts?.length === 2 && action.parts.every((x) => x.trim()),
      "Укажите два случая.",
    );
    const ids = action.parts.map((title, i) => {
      const id = "C-" + next + "-" + i;
      data.entities.push(
        entity(id, title.trim(), "claim", item.study, "open", title.trim(), {
          assumptions: item.assumptions,
          author: "user",
        }),
      );
      data.workLinks.push([item.id, id]);
      return id;
    });
    const coverageId = "O-" + next;
    data.entities.push(
      entity(
        coverageId,
        "Покрытие нового разбиения",
        "obligation",
        item.study,
        "open",
        "Доказать, что предложенные случаи покрывают исходную область.",
        { author: "user" },
      ),
    );
    data.workLinks.push([item.id, coverageId]);
    item.dependencies.push(...ids, coverageId);
    item.revision += 1;
    item.status = "open";
    label = "Предложено разбиение на случаи";
    detail =
      "Созданы два подутверждения и отдельное обязательство покрытия. Исходная цель остается открытой.";
  } else if (action.type === "LEMMA") {
    assert(
      action.title?.trim() &&
        action.statement?.trim() &&
        action.assumptions?.trim(),
      "Заполните название, утверждение и условия.",
    );
    const id = "L-" + next;
    data.entities.push(
      entity(
        id,
        action.title.trim(),
        "lemma",
        action.study || "forest",
        "open",
        action.statement.trim(),
        { assumptions: action.assumptions.trim(), author: "user" },
      ),
    );
    target = id;
    label = "Добавлена лемма-кандидат";
    detail = "Лемма сохранена без статуса приемки.";
  } else if (action.type === "APPLY") {
    const lemma = data.entities.find((x) => x.id === action.lemma);
    assert(
      lemma?.kind === "lemma" && effectiveStatus(data, lemma.id) === "accepted",
      "Для применения выберите действующую принятую лемму.",
    );
    assert(item && item.id !== lemma.id, "Выберите целевое утверждение.");
    assert(
      ["goal", "claim"].includes(item.kind) &&
        !["accepted", "refuted"].includes(item.status),
      "Принятая или опровергнутая версия не изменяется новым применением.",
    );
    assert(
      !data.applications.some(
        (x) => x.lemma === lemma.id && x.target === item.id,
      ),
      "Такое применение уже предложено.",
    );
    const id = "A-" + next;
    data.entities.push(
      entity(
        id,
        "Применимость " + lemma.id,
        "application",
        item.study,
        "in_review",
        "Проверить область, предпосылки и перенос результата " +
          lemma.id +
          " к " +
          item.id +
          ".",
        {
          dependencies: [lemma.id],
          assumptions: item.assumptions,
          author: "agent",
          proof:
            "Предложение применения. Проверка подстановки и всех предпосылок требуется отдельно.",
        },
      ),
    );
    data.applications.push({
      id,
      lemma: lemma.id,
      lemmaRevision: lemma.revision,
      target: item.id,
      state: "candidate",
    });
    item.dependencies.push(id);
    data.workLinks.push([item.id, id]);
    addTask(
      id,
      "Проверить перенос " + lemma.id + " к " + item.id,
      "R03",
      "Проверяющий",
    );
    target = id;
    label = "Предложено применение леммы";
    detail =
      "Создано отдельное обязательство применимости. Целевая гипотеза не принята автоматически.";
  } else if (action.type === "SUBMIT_REVIEW") {
    assert(
      item && item.status === "open",
      "На проверку можно направить открытый кандидат.",
    );
    item.status = "in_review";
    addTask(item.id, "Рецензия: " + item.title, "R03", "Проверяющий");
    label = "Запрошена рецензия";
    detail = "Проверка сохранена в очереди макета.";
  } else if (action.type === "REVIEW") {
    assert(
      item?.status === "in_review",
      "Кандидат должен находиться на проверке.",
    );
    assert(
      item.author !== "user",
      "Авторское предложение требует отдельного проверяющего.",
    );
    if (action.decision === "accept") {
      assert(
        !data.findings.some(
          (x) =>
            x.target === item.id &&
            x.severity !== "editorial" &&
            x.state !== "resolved",
        ),
        "Есть незакрытое математическое замечание.",
      );
      assert(
        item.kind !== "application",
        "Перенос в макете принимается только заданным событием сценария.",
      );
      assert(
        item.dependencies.every(
          (id) => effectiveStatus(data, id) === "accepted",
        ),
        "Есть незакрытые или оспоренные основания.",
      );
      item.status = "accepted";
      label = "Записана приемка демонстрационного кандидата";
    } else {
      item.status = "needs_changes";
      label = "Запрошены исправления";
    }
    data.tasks
      .filter((x) => x.target === item.id && x.state !== "done")
      .forEach((x) => {
        x.state = "done";
      });
    detail =
      "Решение относится только к демонстрационным данным. Реальная проверка доказательства не выполнялась.";
  } else if (action.type === "FINDING") {
    assert(item && action.text?.trim(), "Укажите предмет замечания.");
    data.findings.push({
      id: "R-" + next,
      target: item.id,
      text: action.text.trim(),
      severity: action.severity || "question",
      state: "open",
      revision: item.revision,
    });
    label = "Записано замечание";
    detail = action.text.trim();
  } else if (action.type === "CHALLENGE") {
    assert(
      item && item.status === "accepted" && action.text?.trim(),
      "Укажите причину пересмотра принятого результата.",
    );
    item.status = "challenged";
    data.findings.push({
      id: "R-" + next,
      target: item.id,
      text: action.text.trim(),
      severity: "major",
      state: "open",
      revision: item.revision,
    });
    label = "Оспорено основание";
    detail =
      "Зависимые результаты требуют пересмотра. Ложность утверждения не заявлена.";
  } else if (action.type === "RESOLVE_FINDING") {
    const finding = data.findings.find((x) => x.id === action.finding);
    assert(finding && finding.state === "open", "Замечание не найдено.");
    finding.state = "verification_pending";
    label = "Запрошена повторная проверка";
    target = finding.target;
    detail = "Авторский ответ не закрывает замечание автоматически.";
  } else if (action.type === "STEP") {
    assert(!data.paused, "Поиск приостановлен.");
    actor = "Сценарий";
    if (data.scenarioStep === 0) {
      const claim = data.entities.find((x) => x.id === "H1");
      assert(
        ["in_review", "accepted"].includes(claim.status),
        "Для H1 запрошены исправления; сценарий не заменяет их проверку.",
      );
      assert(
        effectiveStatus(data, "L1") === "accepted",
        "Основание L1 требует пересмотра.",
      );
      assert(
        !data.findings.some((x) => x.target === "H1" && x.state !== "resolved"),
        "У H1 есть открытое замечание.",
      );
      claim.status = "accepted";
      data.tasks.find((x) => x.id === "T1").state = "done";
      target = "H1";
      label = "Принят связный случай";
      detail =
        "Учебное событие: проверяющий завершил разбор индукции. Общая гипотеза остается открытой.";
    } else if (data.scenarioStep === 1) {
      const exists = data.applications.find(
        (x) => x.lemma === "L2" && x.target === "H3",
      );
      if (!exists) {
        const partial = transition(state, {
          type: "APPLY",
          lemma: "L2",
          target: "H3",
        });
        const updated = { ...partial, scenarioStep: 2 };
        updated.history.at(-1).snapshot.scenarioStep = 2;
        return updated;
      }
      target = exists.id;
      label = "Найдено существующее применение";
      detail = "Повторная запись не создавалась.";
    } else if (data.scenarioStep === 2) {
      const application = data.applications.find(
        (x) => x.lemma === "L2" && x.target === "H3",
      );
      assert(
        application && effectiveStatus(data, "L2") === "accepted",
        "Применение L2 недоступно или его основание оспорено.",
      );
      assert(
        !data.findings.some(
          (x) => x.target === application.id && x.state !== "resolved",
        ),
        "Применение имеет открытое замечание.",
      );
      data.entities.find((x) => x.id === application.id).status = "accepted";
      application.state = "accepted";
      data.tasks
        .filter((task) => task.target === application.id)
        .forEach((task) => {
          task.state = "done";
        });
      target = application.id;
      label = "Проверено применение общей леммы";
      detail =
        "Учебное событие: совпали область и определения. Для H3 еще требуется переход от среднего.";
    } else {
      throw new Error("Все три события сценария уже показаны.");
    }
    data.scenarioStep += 1;
  } else {
    throw new Error("Неизвестное действие.");
  }
  data.revision = next;
  return {
    ...data,
    history: [
      ...state.history,
      {
        id: next,
        time: new Intl.DateTimeFormat("ru", {
          hour: "2-digit",
          minute: "2-digit",
        }).format(new Date()),
        label,
        detail,
        target,
        actor,
        snapshot: clone(data),
      },
    ],
  };
}

export function isStoredState(value) {
  return (
    value?.schema === 1 &&
    Number.isInteger(value.revision) &&
    Array.isArray(value.entities) &&
    value.entities.some((x) => x.id === "H0") &&
    value.entities.every(
      (x) => x && typeof x.id === "string" && Array.isArray(x.dependencies),
    ) &&
    [
      "history",
      "studies",
      "tasks",
      "questions",
      "findings",
      "applications",
      "workLinks",
    ].every((k) => Array.isArray(value[k])) &&
    value.history.every(
      (x) =>
        x?.snapshot?.revision === x.id && Array.isArray(x.snapshot.entities),
    )
  );
}
