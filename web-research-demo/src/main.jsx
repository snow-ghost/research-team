import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { createRoot } from "react-dom/client";
import { verificationCurrent } from "./proof-state.js";
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  BaseEdge,
  getSmoothStepPath,
  Handle,
  Position,
  MarkerType,
  useReactFlow,
} from "@xyflow/react";
import ELK from "elkjs/lib/elk.bundled.js";
import {
  Network,
  Library,
  ClipboardCheck,
  History,
  Search,
  Pause,
  Play,
  StepForward,
  Plus,
  X,
  ChevronRight,
  GitBranch,
  FlaskConical,
  MessageSquare,
  Check,
  ArrowUpRight,
  Download,
  RotateCcw,
  ZoomIn,
  ZoomOut,
  Maximize,
  CircleCheck,
  CircleHelp,
  CircleAlert,
  Clock3,
  BookOpen,
  Layers3,
  ListTodo,
  Send,
  ArrowLeft,
  ShieldCheck,
  Link2,
  PanelRightClose,
  Menu,
  Cpu,
  LogOut,
} from "lucide-react";
import { Connected } from "./connected.jsx";
import { RuntimePanel } from "./runtime.jsx";
import { ProfileFactory, StructuredLemmaSearch, ComparisonPanel } from "./research-management.jsx";
import { SkillRegistry } from "./skills.jsx";
import { CoddyPanel } from "./coddy.jsx";
import {
  createInitialState,
  effectiveStatus,
  transition,
  isStoredState,
  STORAGE_KEY,
  statuses,
  kinds,
} from "./model.js";
import "@xyflow/react/dist/style.css";
import "./style.css";

const icons = {
  goal: Network,
  claim: GitBranch,
  lemma: BookOpen,
  obligation: ClipboardCheck,
  application: Link2,
};
const taskStates = {
  queued: "В очереди",
  running: "В работе",
  review: "Рецензия",
  done: "Завершено",
};
const elk = new ELK();

function IconButton({ icon: Icon, label, ...props }) {
  return (
    <button className="icon-button" title={label} aria-label={label} {...props}>
      <Icon size={17} />
    </button>
  );
}
function Badge({ status }) {
  const Icon =
    status === "accepted"
      ? CircleCheck
      : ["refuted", "challenged", "blocked", "needs_changes"].includes(status)
        ? CircleAlert
        : status === "in_review"
          ? Clock3
          : CircleHelp;
  return (
    <span className={"badge " + status}>
      <Icon size={12} />
      {statuses[status]}
    </span>
  );
}
function ResearchNode({ data, selected }) {
  const Icon = icons[data.item.kind] || Layers3;
  return (
    <div
      className={"research-node " + (selected ? "selected " : "") + data.status}
      data-entity={data.item.id}
      title={data.item.title}
    >
      <Handle
        type="target"
        position={data.direction === "LR" ? Position.Left : Position.Top}
      />
      <div className="node-meta">
        <span>
          <Icon size={13} />
          {kinds[data.item.kind]}
        </span>
        <code title={data.item.id}>{data.item.id}</code>
      </div>
      <div className="node-title">{data.item.title}</div>
      <Badge status={data.status} />
      <Handle
        type="source"
        position={data.direction === "LR" ? Position.Right : Position.Bottom}
      />
    </div>
  );
}
function RuleNode({ data }) {
  return (
    <div className="rule-node">
      <Handle
        type="target"
        position={data.direction === "LR" ? Position.Left : Position.Top}
      />
      <Layers3 size={13} />
      <span>{data.label}</span>
      <Handle
        type="source"
        position={data.direction === "LR" ? Position.Right : Position.Bottom}
      />
    </div>
  );
}
const nodeTypes = { research: ResearchNode, rule: RuleNode };

function RoutedEdge(props) {
  const sections = props.data?.sections;
  const path = sections?.length
    ? sections
        .map((section) =>
          [section.startPoint, ...(section.bendPoints || []), section.endPoint]
            .map(
              (point, index) =>
                (index === 0 ? "M" : "L") + point.x + "," + point.y,
            )
            .join(" "),
        )
        .join(" ")
    : getSmoothStepPath({ ...props, offset: 8, borderRadius: 4 })[0];
  return (
    <BaseEdge
      id={props.id}
      path={path}
      markerEnd={props.markerEnd}
      style={props.style}
    />
  );
}
const edgeTypes = { routed: RoutedEdge };

function graphData(state, studyId, mode, rejected) {
  const study = state.studies.find((s) => s.id === studyId);
  if (!study) return { nodes: [], edges: [], direction: "DOWN" };
  const byId = Object.fromEntries(state.entities.map((x) => [x.id, x]));
  let links = [];
  if (mode === "branches") {
    links = [
      ...state.workLinks,
      ...state.applications.map((x) => [x.id, x.lemma]),
    ];
  } else {
    links = state.entities.flatMap((x) => x.dependencies.map((d) => [x.id, d]));
  }
  const visible = new Set();
  function visit(id) {
    if (
      visible.has(id) ||
      !byId[id] ||
      (!rejected && byId[id].status === "refuted")
    )
      return;
    visible.add(id);
    links.filter(([p]) => p === id).forEach(([, c]) => visit(c));
  }
  visit(study.goal);
  if (rejected && studyId === "forest") visible.add("H-bad");
  const direction = "DOWN";
  const nodes = [...visible].map((id) => ({
    id,
    type: "research",
    data: {
      item: byId[id],
      status: effectiveStatus(state, id),
      direction: direction === "RIGHT" ? "LR" : "TB",
    },
    width: 224,
    height: 108,
  }));
  const edges = [];
  const edge = (source, target, dashed = false) =>
    edges.push({
      id: source + ":" + target,
      source,
      target,
      type: "routed",
      markerEnd: {
        type: MarkerType.ArrowClosed,
        width: 16,
        height: 16,
        color: dashed ? "#bf7670" : "#8d9aa7",
      },
      style: {
        stroke: dashed ? "#bf7670" : "#8d9aa7",
        strokeWidth: 1.5,
        strokeDasharray: dashed ? "5 4" : undefined,
      },
    });
  if (mode === "branches") {
    for (const id of visible) {
      const children = links.filter(([p, c]) => p === id && visible.has(c));
      const regular = children.filter(([, c]) => byId[c].status !== "refuted");
      if (regular.length > 1) {
        const ruleId = "rule-" + id;
        nodes.push({
          id: ruleId,
          type: "rule",
          data: { label: "Все обязательства", direction: "TB", sourceId: id },
          width: 166,
          height: 36,
        });
        edge(id, ruleId);
        regular.forEach(([, c]) => edge(ruleId, c));
      } else regular.forEach(([, c]) => edge(id, c));
      children
        .filter(([, c]) => byId[c].status === "refuted")
        .forEach(([, c]) => edge(id, c, true));
    }
  } else {
    for (const id of visible) {
      const deps = byId[id].dependencies.filter((d) => visible.has(d));
      if (!deps.length) continue;
      const ruleId = "derive-" + id;
      nodes.push({
        id: ruleId,
        type: "rule",
        data: { label: "Обоснование", direction: "TB", sourceId: id },
        width: 132,
        height: 36,
      });
      deps.forEach((d) => edge(d, ruleId));
      edge(ruleId, id);
    }
  }
  return { nodes, edges, direction };
}

function Graph({ state, study, mode, rejected, selected, onSelect }) {
  const raw = useMemo(
    () => graphData(state, study, mode, rejected),
    [state, study, mode, rejected],
  );
  const topology = JSON.stringify([
    raw.direction,
    raw.nodes.map((x) => x.id),
    raw.edges.map((x) => x.id),
  ]);
  const [layout, setLayout] = useState({ key: "", positions: {}, routes: {} });
  const flow = useReactFlow();
  useEffect(() => {
    let active = true;
    elk
      .layout({
        id: "root",
        layoutOptions: {
          "elk.algorithm": "layered",
          "elk.direction": raw.direction,
          "elk.edgeRouting": "ORTHOGONAL",
          "elk.spacing.nodeNode": "38",
          "elk.layered.spacing.nodeNodeBetweenLayers":
            mode === "proof" ? "24" : "48",
        },
        children: raw.nodes.map((n) => ({
          id: n.id,
          width: n.width,
          height: n.height,
          layoutOptions: { "elk.portConstraints": "FIXED_POS" },
          ports: [
            {
              id: n.id + "-in",
              x: n.width / 2,
              y: 0,
              width: 0,
              height: 0,
              layoutOptions: { "elk.port.side": "NORTH" },
            },
            {
              id: n.id + "-out",
              x: n.width / 2,
              y: n.height,
              width: 0,
              height: 0,
              layoutOptions: { "elk.port.side": "SOUTH" },
            },
          ],
        })),
        edges: raw.edges.map((e) => ({
          id: e.id,
          sources: [e.source + "-out"],
          targets: [e.target + "-in"],
        })),
      })
      .then((result) => {
        if (active)
          setLayout({
            key: topology,
            positions: Object.fromEntries(
              result.children.map((n) => [n.id, { x: n.x, y: n.y }]),
            ),
            routes: Object.fromEntries(
              result.edges.map((e) => [e.id, e.sections]),
            ),
          });
      })
      .catch(() => {
        if (active)
          setLayout({
            key: topology,
            routes: {},
            positions: Object.fromEntries(
              raw.nodes.map((n, i) => [
                n.id,
                { x: (i % 3) * 270, y: Math.floor(i / 3) * 170 },
              ]),
            ),
          });
      });
    return () => {
      active = false;
    };
  }, [topology]);
  useEffect(() => {
    if (layout.key !== topology) return;
    const timer = setTimeout(
      () =>
        flow.fitView({
          padding: mode === "proof" ? 0.07 : 0.16,
          maxZoom: 1.1,
          duration: 180,
        }),
      50,
    );
    return () => clearTimeout(timer);
  }, [layout.key, topology, flow, mode]);
  const nodes = useMemo(
    () =>
      raw.nodes.map((n) => ({
        ...n,
        position: layout.positions[n.id] || { x: 0, y: 0 },
        selected: n.id === selected,
      })),
    [raw.nodes, layout.positions, selected],
  );
  const onNodesChange = useCallback(
    (changes) => {
      const chosen = changes.find(
        (change) => change.type === "select" && change.selected,
      );
      if (
        chosen &&
        chosen.id !== selected &&
        raw.nodes.find((n) => n.id === chosen.id)?.data.item
      )
        onSelect(chosen.id);
    },
    [selected, raw.nodes, onSelect],
  );
  const edges = useMemo(
    () =>
      raw.edges.map((edge) => ({
        ...edge,
        data: {
          sections:
            layout.key === topology ? layout.routes[edge.id] : undefined,
        },
      })),
    [raw.edges, layout.key, layout.routes, topology],
  );
  return (
    <div
      className="graph"
      data-layout={layout.key === topology ? "ready" : "pending"}
    >
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onNodeClick={(_, node) =>
          onSelect(node.data.item?.id || node.data.sourceId)
        }
        onNodesChange={onNodesChange}
        nodesDraggable={false}
        nodesConnectable={false}
        edgesReconnectable={false}
        deleteKeyCode={null}
        minZoom={0.25}
        maxZoom={1.6}
        proOptions={{ hideAttribution: false }}
        ariaLabelConfig={{
          "controls.ariaLabel": "Масштаб карты",
          "node.a11yDescription.default":
            "Узел исследования. Выберите для просмотра.",
          "edge.a11yDescription.default": "Связь объектов исследования.",
        }}
      >
        <Background color="#dce2e6" gap={22} size={1} />
      </ReactFlow>
      <div className="map-caption">
        <span className="live-dot" />
        {mode === "branches"
          ? "От цели к обязательствам"
          : "От посылок к заключению"}
      </div>
      <div className="map-controls">
        <IconButton
          icon={ZoomOut}
          label="Уменьшить"
          onClick={() => flow.zoomOut()}
        />
        <IconButton
          icon={ZoomIn}
          label="Увеличить"
          onClick={() => flow.zoomIn()}
        />
        <IconButton
          icon={Maximize}
          label="Показать всю карту"
          onClick={() =>
            flow.fitView({
              padding: mode === "proof" ? 0.07 : 0.16,
              maxZoom: 1.1,
              duration: 180,
            })
          }
        />
      </div>
      {layout.key !== topology && (
        <div className="layout-loading">Расположение узлов...</div>
      )}
    </div>
  );
}

function ClaimList({ state, study, rejected, selected, onSelect }) {
  const root = state.studies.find((x) => x.id === study).goal;
  const rows = [],
    visited = new Set();
  const links = [
    ...state.workLinks,
    ...state.applications.map((x) => [x.id, x.lemma]),
  ];
  function add(id, depth) {
    const item = state.entities.find((x) => x.id === id);
    if (!item || visited.has(id) || (!rejected && item.status === "refuted"))
      return;
    visited.add(id);
    rows.push({ item, depth });
    links.filter(([p]) => p === id).forEach(([, c]) => add(c, depth + 1));
  }
  add(root, 0);
  return (
    <div className="claim-list" aria-label="Ветви списком">
      {rows.map(({ item, depth }) => (
        <button
          key={item.id}
          className={"claim-list-row " + (selected === item.id ? "chosen" : "")}
          style={{ marginLeft: Math.min(depth, 2) * 12 }}
          onClick={() => onSelect(item.id)}
        >
          <div>
            <span>
              {kinds[item.kind]} <code>{item.id}</code>
            </span>
            <strong>{item.title}</strong>
            <Badge status={effectiveStatus(state, item.id)} />
          </div>
          <ChevronRight size={16} />
        </button>
      ))}
    </div>
  );
}

function App({ live } = {}) {
  const [state, setState] = useState(() => {
    if (live) return live.data.state;
    try {
      const value = JSON.parse(localStorage.getItem(STORAGE_KEY));
      return isStoredState(value) ? value : createInitialState();
    } catch {
      return createInitialState();
    }
  });
  const [study, setStudy] = useState(() =>
    live ? live.data.state.studies[0]?.id || "" : "forest",
  );
  const [section, setSection] = useState("research");
  const [tab, setTab] = useState("map");
  const [mode, setMode] = useState(() =>
    window.innerWidth <= 640 ? "list" : "branches",
  );
  const [selected, setSelected] = useState(() =>
    live ? live.data.state.studies[0]?.goal || "" : "H0",
  );
  const [detailTab, setDetailTab] = useState("overview");
  const [rejected, setRejected] = useState(false);
  const [lemmaFilter, setLemmaFilter] = useState("accepted");
  const [query, setQuery] = useState("");
  const [modal, setModal] = useState(null);
  const [toast, setToast] = useState("");
  const [historic, setHistoric] = useState(null);
  const [historicData, setHistoricData] = useState(null);
  const [mobileNav, setMobileNav] = useState(false);
  const [inspectorOpen, setInspectorOpen] = useState(
    () => window.innerWidth > 1100,
  );
  const [question, setQuestion] = useState("");
  const [questionKind, setQuestionKind] = useState("question");
  const previousFocus = useRef(null);
  const view = historic
    ? historicData ||
      state.history.find((x) => x.id === historic)?.snapshot ||
      state
    : state;
  const currentStudy = view.studies.find((x) => x.id === study) ||
    view.studies[0] || {
      id: "",
      title: "Исследования",
      category: "Рабочая область",
      formula: "",
    };
  const item = view.entities.find((x) => x.id === selected);
  const proofCheck = item && (view.verifications || []).find((v) =>
    v.target === item.id && verificationCurrent(v, item, view) &&
    v.status === "verified" && v.report?.status === "verified" &&
    (item.proofVerification ? v.id === item.proofVerification : item.proofAttempt && v.attempt === item.proofAttempt),
  );
  const readOnly = historic !== null || !!live?.busy;
  const allStudy = view.entities.filter((x) => x.study === currentStudy.id);
  const tasks = view.tasks.filter(
    (t) =>
      view.entities.find((e) => e.id === t.target)?.study === currentStudy.id,
  );
  useEffect(() => {
    if (live) return;
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
    } catch {
      setToast("Не удалось сохранить данные в браузере. Выгрузите снимок.");
    }
  }, [state, !!live]);
  useEffect(() => {
    if (live) setState(live.data.state);
  }, [live?.data.state]);
  useEffect(() => {
    if (
      live &&
      !historic &&
      state.studies.length &&
      !state.studies.some((s) => s.id === study)
    ) {
      setStudy(state.studies[0].id);
      setSelected(state.studies[0].goal);
    }
  }, [live?.data.state, historic, study, state.studies]);
  useEffect(() => {
    if (!toast) return;
    const timer = setTimeout(() => setToast(""), 5000);
    return () => clearTimeout(timer);
  }, [toast]);
  useEffect(() => {
    const handler = (e) => {
      if (e.key === "Escape") {
        setModal(null);
        setMobileNav(false);
        setQuery("");
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);
  const select = useCallback((id) => {
    setSelected(id);
    setInspectorOpen(true);
    setQuery("");
  }, []);
  function openModal(kind, target = selected) {
    previousFocus.current = document.activeElement;
    setModal({ kind, target });
  }
  async function dispatch(action) {
    if (readOnly) {
      setToast("Открыт исторический снимок. Вернитесь к текущему состоянию.");
      return false;
    }
    try {
      const next = live
        ? (await live.action(action)).state
        : transition(state, action);
      setState(next);
      setToast(next.history.at(-1).label);
      if (action.type === "CREATE_STUDY") {
        const created = next.studies.at(-1);
        setStudy(created.id);
        select(created.goal);
        setSection("research");
        setTab("map");
      }
      if (["SPLIT", "APPLY", "LEMMA", "STEP"].includes(action.type)) {
        const target = next.history.at(-1).target;
        const targetItem = next.entities.find((x) => x.id === target);
        if (targetItem) {
          setStudy(targetItem.study);
          select(target);
        }
      }
      return true;
    } catch (error) {
      setToast(error.message);
      return false;
    }
  }
  function closeModal() {
    setModal(null);
    setTimeout(() => previousFocus.current?.focus(), 0);
  }
  function exportState() {
    const exported = {
      ...view,
      history: state.history.filter((event) => event.id <= view.revision),
      demonstration: !live,
      historical: readOnly,
      exported_at: new Date().toISOString(),
    };
    const url = URL.createObjectURL(
      new Blob([JSON.stringify(exported, null, 2)], {
        type: "application/json",
      }),
    );
    const a = document.createElement("a");
    a.href = url;
    a.download =
      "research-team-" + (live ? "" : "demo-") + "v" + view.revision + ".json";
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  const navigation = [
    ["research", Network, "Исследования"],
    ["library", Library, "Библиотека лемм"],
    ["reviews", ClipboardCheck, "Рецензирование"],
    ["history", History, "Журнал действий"],
    ...(live
      ? [
          ["executors", Cpu, "Исполнители"],
          ["coddy", GitBranch, "Coddy Bot"],
        ]
      : []),
  ];
  const sectionTitle =
    section === "research"
      ? currentStudy.title
      : navigation.find((x) => x[0] === section)[2];
  const matches = query.trim()
    ? view.entities.filter((x) =>
        (x.title + " " + x.id + " " + x.statement)
          .toLowerCase()
          .includes(query.toLowerCase()),
      )
    : [];
  const confirmReview = (decision) =>
    openModal(decision === "accept" ? "accept" : "reject", selected);
  return (
    <div className="app-shell">
      <aside className={"sidebar " + (mobileNav ? "mobile-open" : "")}>
        <div className="brand">
          <div className="brand-symbol">
            <Network size={23} />
          </div>
          <div>
            <strong>Исследования</strong>
            <span>Рабочая область</span>
          </div>
          <IconButton
            className="icon-button mobile-only"
            icon={X}
            label="Закрыть меню"
            onClick={() => setMobileNav(false)}
          />
        </div>
        <div className="demo-label">
          <span />
          {live ? "Серверная рабочая область" : "Демонстрационные данные"}
        </div>
        <nav aria-label="Главные разделы">
          {navigation.map(([id, Icon, label]) => (
            <button
              key={id}
              disabled={
                !!live &&
                historic !== null &&
                ["executors", "coddy"].includes(id)
              }
              className={"nav-item " + (section === id ? "active" : "")}
              onClick={() => {
                setSection(id);
                if (["executors", "library"].includes(id)) setInspectorOpen(false);
                setMobileNav(false);
              }}
            >
              <Icon size={17} />
              <span>{label}</span>
              {id === "reviews" && (
                <small>
                  {view.entities.filter((x) => x.status === "in_review").length}
                </small>
              )}
            </button>
          ))}
        </nav>
        <div className="sidebar-heading">ТЕКУЩИЕ ИССЛЕДОВАНИЯ</div>
        {live && (
          <button
            className="button ghost new-study"
            disabled={readOnly}
            onClick={() => openModal("study")}
          >
            <Plus size={15} />
            Новое исследование
          </button>
        )}
        <div className="study-list">
          {view.studies.map((s) => (
            <button
              key={s.id}
              className={
                study === s.id && section === "research" ? "chosen" : ""
              }
              onClick={() => {
                setStudy(s.id);
                setSection("research");
                setTab("map");
                select(s.goal);
                setMobileNav(false);
              }}
            >
              <span className="study-dot" />
              <span>{s.title}</span>
              <ChevronRight size={14} />
            </button>
          ))}
        </div>
        <div className="sidebar-bottom">
          <div className="local-state">
            <span className="live-dot" />
            {live ? "Серверный" : "Локальный"} снимок v{state.revision}
          </div>
          <button onClick={exportState}>
            <Download size={16} />
            Выгрузить снимок
          </button>
          {!live && (
            <button disabled={readOnly} onClick={() => openModal("reset")}>
              <RotateCcw size={16} />
              Сбросить макет
            </button>
          )}
          {live && (
            <button
              disabled={live.busy}
              onClick={() => live.logout().catch((e) => setToast(e.message))}
            >
              <LogOut size={16} />
              Выйти
            </button>
          )}
          <div className="workspace-user">
            <span className="avatar">В</span>
            <div>
              <strong>Вы</strong>
              <span>{live ? "Оператор" : "Демонстрационная среда"}</span>
            </div>
          </div>
        </div>
      </aside>
      {mobileNav && (
        <button
          className="nav-shade"
          aria-label="Закрыть навигацию"
          onClick={() => setMobileNav(false)}
        />
      )}
      <main className="workspace">
        <header className="topbar">
          <div className="breadcrumb">
            <IconButton
              icon={Menu}
              label="Открыть меню"
              className="icon-button mobile-only"
              onClick={() => setMobileNav(true)}
            />
            <span>Исследования</span>
            {!live && <span className="topbar-demo">Макет</span>}
            <ChevronRight size={13} />
            <strong>
              {section === "research" ? currentStudy.category : sectionTitle}
            </strong>
          </div>
          <div className="search-wrap">
            <Search size={16} />
            <input
              aria-label="Поиск по утверждениям"
              placeholder="Найти утверждение..."
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
            {query && (
              <IconButton
                icon={X}
                label="Очистить поиск"
                onClick={() => setQuery("")}
              />
            )}
            {query && (
              <div className="search-results">
                {matches.length ? (
                  matches.map((x) => (
                    <button
                      key={x.id}
                      onClick={() => {
                        setStudy(x.study);
                        select(x.id);
                      }}
                    >
                      <code>{x.id}</code>
                      <span>{x.title}</span>
                      <ArrowUpRight size={14} />
                    </button>
                  ))
                ) : (
                  <div className="empty-small">Совпадений нет</div>
                )}
              </div>
            )}
          </div>
        </header>
        {live?.error && (
          <div className="connection-banner" role="alert">
            {live.error}
          </div>
        )}
        {live && !live.data.operational && (
          <div className="connection-banner" role="alert">
            Исполнение остановлено после ошибки хранения. Нужна проверка
            сервера.
          </div>
        )}
        {readOnly && (
          <div className="history-banner">
            <History size={16} />
            <span>Исторический снимок v{historic}. Только чтение.</span>
            <button
              onClick={() => {
                setHistoric(null);
                setHistoricData(null);
                select(currentStudy.goal);
              }}
            >
              К текущему состоянию <ArrowUpRight size={14} />
            </button>
          </div>
        )}
        <section className="page-header">
          <div>
            <div className="eyebrow">
              {section === "research"
                ? live
                  ? "ИССЛЕДОВАНИЕ"
                  : "ИССЛЕДОВАНИЕ / " + (study === "forest" ? "01" : "02")
                : "РАБОЧАЯ ОБЛАСТЬ"}
            </div>
            <h1>{sectionTitle}</h1>
            {section === "research" && (
              <div className="goal-line">
                {!live && <code>{currentStudy.formula}</code>}
                <span>
                  {live
                    ? "Снимок v" + view.revision
                    : study === "forest"
                      ? "n >= 1; c >= 1"
                      : "n > 0"}
                </span>
                <span className={"run-state " + (view.paused ? "paused" : "")}>
                  <span />
                  {view.paused
                    ? live ? "Новые запуски приостановлены" : "Поиск приостановлен"
                    : "Исследование открыто"}
                </span>
              </div>
            )}
          </div>
          <div className="page-actions">
            {section === "research" && (
              <>
                <button
                  className="button secondary"
                  disabled={readOnly}
                  title={
                    live
                      ? "Приостановка допуска новых запусков; текущие попытки продолжаются"
                      : undefined
                  }
                  onClick={() => dispatch({ type: "PAUSE" })}
                >
                  {view.paused ? <Play size={15} /> : <Pause size={15} />}
                  <span>{view.paused ? "Продолжить" : "Приостановить"}</span>
                </button>
                {!live && (
                  <IconButton
                    icon={StepForward}
                    label="Следующее событие сценария"
                    disabled={
                      readOnly || view.paused || state.scenarioStep >= 3
                    }
                    onClick={() => dispatch({ type: "STEP" })}
                  />
                )}
              </>
            )}
            <button
              className="button primary"
              disabled={readOnly || (live && !currentStudy.goal)}
              onClick={() => openModal("lemma")}
            >
              <Plus size={16} />
              Предложить лемму
            </button>
          </div>
        </section>
        <div className="work-area">
          <div className="main-pane">
            {live && section === "executors" && <ProfileFactory live={live} />}
            {live && section === "executors" && <SkillRegistry live={live} />}
            {live && section === "executors" && <ComparisonPanel live={live} />}
            {live && section === "library" && <StructuredLemmaSearch live={live} target={selected} />}
            {live && section === "executors" && (
              <RuntimePanel
                live={live}
                study={currentStudy.id}
                selected={selected}
                providers
                onSelect={select}
              />
            )}
            {live && section === "coddy" && <CoddyPanel live={live} onTarget={setSelected} />}
            {section === "research" && (
              <>
                <div
                  className="view-tabs"
                  role="tablist"
                  aria-label="Представление исследования"
                >
                  {[
                    ["map", Network, "Карта"],
                    ["tasks", ListTodo, "Задания"],
                    ["discussion", MessageSquare, "Обсуждение"],
                  ].map(([id, Icon, label]) => (
                    <button
                      role="tab"
                      aria-selected={tab === id}
                      className={tab === id ? "active" : ""}
                      key={id}
                      onClick={() => setTab(id)}
                    >
                      <Icon size={15} />
                      {label}
                      {id === "tasks" && <small>{tasks.length}</small>}
                    </button>
                  ))}
                  <button
                    className="inspect-toggle"
                    onClick={() => setInspectorOpen(!inspectorOpen)}
                    title="Сведения об объекте"
                    aria-label="Сведения об объекте"
                  >
                    <BookOpen size={16} />
                  </button>
                </div>
                {tab === "map" && live && !currentStudy.goal && (
                  <div className="workspace-empty">
                    <Network size={32} />
                    <h2>Исследований пока нет</h2>
                    <button
                      className="button primary"
                      onClick={() => openModal("study")}
                    >
                      <Plus size={16} />
                      Создать исследование
                    </button>
                  </div>
                )}
                {tab === "map" && (!live || currentStudy.goal) && (
                  <>
                    <div className="map-toolbar">
                      <div className="segmented">
                        <button
                          className={mode === "list" ? "selected" : ""}
                          onClick={() => setMode("list")}
                        >
                          Список
                        </button>
                        <button
                          className={mode === "branches" ? "selected" : ""}
                          onClick={() => setMode("branches")}
                        >
                          Ветви исследования
                        </button>
                        <button
                          className={mode === "proof" ? "selected" : ""}
                          onClick={() => setMode("proof")}
                        >
                          Зависимости
                        </button>
                      </div>
                      <label className="checkbox">
                        <input
                          type="checkbox"
                          checked={rejected}
                          onChange={(e) => setRejected(e.target.checked)}
                        />
                        Отвергнутые
                      </label>
                    </div>
                    {mode === "list" ? (
                      <ClaimList
                        state={view}
                        study={currentStudy.id}
                        rejected={rejected}
                        selected={selected}
                        onSelect={select}
                      />
                    ) : (
                      <ReactFlowProvider>
                        <Graph
                          state={view}
                          study={currentStudy.id}
                          mode={mode}
                          rejected={rejected}
                          selected={selected}
                          onSelect={select}
                        />
                      </ReactFlowProvider>
                    )}
                    <div className="map-footer">
                      <span>
                        <CircleCheck size={14} />
                        {
                          allStudy.filter(
                            (x) => effectiveStatus(view, x.id) === "accepted",
                          ).length
                        }{" "}
                        принятых записей
                      </span>
                      <span>
                        <Clock3 size={14} />
                        {
                          allStudy.filter((x) => x.status === "in_review")
                            .length
                        }{" "}
                        на проверке
                      </span>
                      <span>
                        <CircleAlert size={14} />
                        {
                          allStudy.filter((x) => x.status === "refuted").length
                        }{" "}
                        опровержений
                      </span>
                    </div>
                  </>
                )}
                {tab === "tasks" && live && !historic && (
                  <RuntimePanel
                    live={live}
                    study={currentStudy.id}
                    selected={selected}
                    onSelect={select}
                  />
                )}
                {tab === "tasks" && (!live || historic) && (
                  <div className="scroll-content">
                    <div className="section-intro">
                      <h2>Назначения исполнителей</h2>
                      <span>{tasks.length} заданий</span>
                    </div>
                    <div className="table-scroll">
                      <table>
                        <thead>
                          <tr>
                            <th>Задание</th>
                            <th>Исполнитель</th>
                            <th>Метод</th>
                            <th>Состояние</th>
                          </tr>
                        </thead>
                        <tbody>
                          {tasks.map((t) => (
                            <tr key={t.id} onClick={() => select(t.target)}>
                              <td>
                                <button
                                  className="text-link"
                                  onClick={() => select(t.target)}
                                >
                                  {t.title}
                                </button>
                                <small>
                                  {t.id} · {t.target}
                                </small>
                              </td>
                              <td>{t.agent}</td>
                              <td>
                                <code>{t.skill}</code>
                              </td>
                              <td>
                                <span className={"task-state " + t.state}>
                                  {taskStates[t.state] || t.state}
                                </span>
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  </div>
                )}
                {tab === "discussion" && (
                  <div className="scroll-content">
                    <div className="section-intro">
                      <h2>Вопросы к исследованию</h2>
                      <button
                        className="button secondary"
                        disabled={readOnly}
                        onClick={() => {
                          select(currentStudy.goal);
                          setDetailTab("questions");
                        }}
                      >
                        <Plus size={15} />
                        Задать вопрос
                      </button>
                    </div>
                    <QuestionList
                      questions={view.questions.filter(
                        (q) =>
                          view.entities.find((x) => x.id === q.target)
                            ?.study === study,
                      )}
                      onSelect={(id) => {
                        select(id);
                        setDetailTab("questions");
                      }}
                    />
                  </div>
                )}
              </>
            )}
            {section === "library" && (
              <div className="scroll-content">
                <div className="section-intro">
                  <h2>Общие результаты</h2>
                  <select
                    aria-label="Состояние лемм"
                    value={lemmaFilter}
                    onChange={(e) => setLemmaFilter(e.target.value)}
                  >
                    <option value="accepted">Принятые</option>
                    <option value="all">Все состояния</option>
                  </select>
                </div>
                <div className="table-scroll">
                  <table>
                    <thead>
                      <tr>
                        <th>Лемма</th>
                        <th>Область</th>
                        <th>Состояние</th>
                        <th>Применения</th>
                        <th />
                      </tr>
                    </thead>
                    <tbody>
                      {view.entities
                        .filter(
                          (x) =>
                            (x.kind === "lemma" || (view.library || []).some(l => l.lemma === x.id && l.status === "ready")) &&
                            (lemmaFilter === "all" ||
                              effectiveStatus(view, x.id) === "accepted"),
                        )
                        .map((x) => (
                          <tr
                            key={x.id}
                            className={selected === x.id ? "selected-row" : ""}
                          >
                            <td>
                              <button
                                className="text-link"
                                onClick={() => select(x.id)}
                              >
                                {x.title}
                              </button>
                              <small>
                                {x.id}@{x.revision}
                              </small>
                            </td>
                            <td>{x.assumptions}</td>
                            <td>
                              <Badge status={effectiveStatus(view, x.id)} />
                            </td>
                            <td>
                              {
                                view.entities.filter((e) =>
                                  e.dependencies.includes(x.id),
                                ).length
                              }
                            </td>
                            <td>
                              <IconButton
                                icon={ArrowUpRight}
                                label={"Применить " + x.id}
                                disabled={
                                  readOnly ||
                                  effectiveStatus(view, x.id) !== "accepted"
                                }
                                onClick={() => openModal("apply", x.id)}
                              />
                            </td>
                          </tr>
                        ))}
                    </tbody>
                  </table>
                </div>
              </div>
            )}
            {section === "reviews" && (
              <div className="scroll-content">
                <div className="section-intro">
                  <h2>Кандидаты и замечания</h2>
                  <span>{view.findings.length} замечаний</span>
                </div>
                {view.entities
                  .filter(
                    (x) =>
                      x.status === "in_review" ||
                      x.status === "challenged" ||
                      x.status === "needs_changes",
                  )
                  .map((x) => (
                    <button
                      key={x.id}
                      className="review-row"
                      onClick={() => {
                        select(x.id);
                        setDetailTab("proof");
                      }}
                    >
                      <span className="review-icon">
                        <ClipboardCheck size={19} />
                      </span>
                      <div>
                        <strong>{x.title}</strong>
                        <span>
                          {x.id}@{x.revision} ·{" "}
                          {x.author === "user"
                            ? "Ваше предложение"
                            : "Исследователь"}
                        </span>
                      </div>
                      <Badge status={effectiveStatus(view, x.id)} />
                      <ChevronRight size={15} />
                    </button>
                  ))}
                <h3 className="subheading">Замечания</h3>
                {view.findings.length ? (
                  view.findings.map((f) => (
                    <article className="finding" key={f.id}>
                      <div>
                        <button
                          className="text-link"
                          onClick={() => select(f.target)}
                        >
                          {f.target}@{f.revision}
                        </button>
                        <span>
                          {f.severity === "major"
                            ? "Существенное"
                            : f.severity === "editorial"
                              ? "Изложение"
                              : "Вопрос"}
                        </span>
                      </div>
                      <p>{f.text}</p>
                      <button
                        className="button secondary"
                        disabled={readOnly || f.state !== "open"}
                        onClick={() =>
                          dispatch({ type: "RESOLVE_FINDING", finding: f.id })
                        }
                      >
                        {f.state === "open"
                          ? "Запросить перепроверку"
                          : f.state === "resolved"
                            ? "Замечание закрыто"
                            : "Ожидает перепроверки"}
                      </button>
                      {live && f.state === "verification_pending" && (
                        <button
                          className="button secondary"
                          disabled={readOnly}
                          onClick={() => openModal("closeFinding", f.id)}
                        >
                          <Check size={14} />
                          Записать результат перепроверки
                        </button>
                      )}
                    </article>
                  ))
                ) : (
                  <Empty
                    icon={ShieldCheck}
                    title="Замечаний пока нет"
                    text="Не проверенные обязательства сохраняют свой статус."
                  />
                )}
              </div>
            )}
            {section === "history" && (
              <div className="scroll-content">
                <div className="section-intro">
                  <h2>История решений</h2>
                  <span>{state.history.length} событий</span>
                </div>
                <div className="timeline">
                  {[...state.history].reverse().map((e) => (
                    <article
                      key={e.id}
                      className={
                        "timeline-entry " +
                        (historic === e.id ? "historical" : "")
                      }
                    >
                      <span className="timeline-point" />
                      <div className="event-meta">
                        <time>{e.time}</time>
                        <span>{e.actor}</span>
                        <code>v{e.id}</code>
                      </div>
                      <h3>{e.label}</h3>
                      <p>{e.detail}</p>
                      <div className="event-actions">
                        <button
                          className="text-link"
                          onClick={() => select(e.target)}
                        >
                          {e.target}
                        </button>
                        <button
                          className="button ghost"
                          onClick={async () => {
                            let snapshot = e.snapshot;
                            if (live) {
                              try {
                                snapshot = await live.get("/history/" + e.id);
                              } catch (error) {
                                setToast(error.message);
                                return;
                              }
                              setHistoricData(snapshot);
                            }
                            setHistoric(e.id);
                            const target =
                              snapshot.tasks.find((x) => x.id === e.target)?.target ||
                              e.target;
                            const t = snapshot.entities.find(
                              (x) => x.id === target,
                            ) || snapshot.entities.find((x) => x.id === selected) ||
                              snapshot.entities[0];
                            setSelected(t?.id || "");
                            if (t) setStudy(t.study);
                            setDetailTab("overview");
                            setSection("research");
                            setTab("map");
                            setInspectorOpen(true);
                          }}
                        >
                          <History size={14} />
                          Открыть снимок
                        </button>
                      </div>
                    </article>
                  ))}
                </div>
              </div>
            )}
          </div>
          {inspectorOpen && item && (
            <aside className="inspector" aria-label="Сведения об объекте">
              <div className="inspector-top">
                <span>
                  <span className="tiny-square" />
                  {kinds[item.kind]}{" "}
                  <code>
                    {item.id}@{item.revision}
                  </code>
                </span>
                <IconButton
                  icon={PanelRightClose}
                  label="Закрыть сведения"
                  onClick={() => setInspectorOpen(false)}
                />
              </div>
              <div className="inspector-heading">
                <h2>{item.title}</h2>
                <Badge status={effectiveStatus(view, item.id)} />
              </div>
              <div className="detail-tabs">
                {[
                  ["overview", "Сведения"],
                  ["proof", "Основания"],
                  ["questions", "Вопросы"],
                ].map(([id, label]) => (
                  <button
                    key={id}
                    className={detailTab === id ? "active" : ""}
                    onClick={() => setDetailTab(id)}
                  >
                    {label}
                  </button>
                ))}
              </div>
              <div className="inspector-body">
                {detailTab === "overview" && (
                  <>
                    <div className="field-label">УТВЕРЖДЕНИЕ</div>
                    <p className="statement">{item.statement}</p>
                    <div className="field-label">ОБЛАСТЬ И УСЛОВИЯ</div>
                    <p>
                      {[item.domain, item.assumptions].filter(Boolean).join(". ")}
                    </p>
                    <div className="metadata">
                      <span>Версия</span>
                      <strong>{item.revision}</strong>
                      <span>Источник</span>
                      <strong>
                        {live
                          ? item.author === "operator"
                            ? "Оператор"
                            : item.author
                          : item.author === "user"
                            ? "Ваше предложение"
                            : "Учебный сценарий"}
                      </strong>
                      <span>Зависимости</span>
                      <strong>{item.dependencies.length}</strong>
                    </div>
                    {item.counterexample && (
                      <div className="counterexample-box">
                        <CircleAlert size={16} />
                        <div>
                          <strong>Проверенный контрпример</strong>
                          <p>{item.counterexample}</p>
                          <span>m = 3 &gt; n - 1 = 2</span>
                        </div>
                      </div>
                    )}
                    <div className="inspector-actions">
                      {item.kind === "lemma" &&
                      effectiveStatus(view, item.id) === "accepted" ? (
                        <>
                          <button
                            className="button primary"
                            disabled={readOnly}
                            onClick={() => openModal("apply")}
                          >
                            <ArrowUpRight size={15} />
                            Применить к гипотезе
                          </button>
                          <button
                            className="button secondary"
                            disabled={readOnly}
                            onClick={() => openModal("challenge")}
                          >
                            <CircleAlert size={15} />
                            Оспорить основание
                          </button>
                        </>
                      ) : (
                        <>
                          <button
                            className="button secondary"
                            disabled={
                              readOnly ||
                              ["accepted", "refuted"].includes(item.status)
                            }
                            onClick={() => openModal("split")}
                          >
                            <GitBranch size={15} />
                            Разбить на случаи
                          </button>
                          <button
                            className="button secondary"
                            disabled={readOnly}
                            onClick={() =>
                              dispatch({
                                type: "COUNTEREXAMPLE",
                                target: item.id,
                              })
                            }
                          >
                            <FlaskConical size={15} />
                            Искать контрпример
                          </button>
                        </>
                      )}
                      <button
                        className="button ghost"
                        onClick={() => setDetailTab("questions")}
                      >
                        <MessageSquare size={15} />
                        Уточнить по этому объекту
                      </button>
                    </div>
                  </>
                )}
                {detailTab === "proof" && (
                  <>
                    <div className="field-label">АРГУМЕНТ</div>
                    {live && item.formal_goal && item.proof
                      ? <pre className="proof-source">{item.proof}</pre>
                      : <p>{item.proof || "Обоснование еще не представлено."}</p>}
                    {live && item.proofAuthor && <p className="muted">Автор: {item.proofAuthor}</p>}
                    {live && item.proofAttempt && (
                      <p className="muted">Источник: <code>{item.proofAttempt}</code></p>
                    )}
                    {live && item.proofVerification && <p className="muted">Проверка: <code>{item.proofVerification}</code></p>}
                    {live && item.reviewReason && (
                      <>
                        <div className="field-label">РЕШЕНИЕ ОПЕРАТОРА</div>
                        <p>{item.reviewReason}</p>
                      </>
                    )}
                    <div className="field-label">ИСПОЛЬЗУЕМЫЕ ОСНОВАНИЯ</div>
                    {item.dependencies.length ? (
                      item.dependencies.map((id) => {
                        const dep = view.entities.find((x) => x.id === id);
                        return (
                          <button
                            key={id}
                            className="dependency"
                            onClick={() => select(id)}
                          >
                            <BookOpen size={15} />
                            <div>
                              <strong>{dep?.title || id}</strong>
                              <span>
                                {id} · {statuses[effectiveStatus(view, id)]}
                              </span>
                            </div>
                            <ChevronRight size={14} />
                          </button>
                        );
                      })
                    ) : (
                      <p className="muted">
                        В этой записи внешние зависимости не указаны.
                      </p>
                    )}
                    <div className="review-notice">
                      <ShieldCheck size={16} />
                      <span>
                        {live
                          ? item.formal_goal
                            ? proofCheck ? "Lean: подтверждено для версии " + item.revision + "." : "Lean: нет успешной проверки текущего доказательства."
                            : "Формальная цель не закреплена."
                          : "Приемка здесь относится только к данным макета."}
                      </span>
                    </div>
                    <div className="inspector-actions">
                      {item.status === "in_review" ? (
                        <>
                          <button
                            className="button primary"
                            disabled={
                              readOnly ||
                              (live
                                ? !item.proofAuthor ||
                                  item.proofAuthor === "operator" || (item.formal_goal && !proofCheck)
                                : item.author === "user" ||
                                  item.kind === "application")
                            }
                            onClick={() => confirmReview("accept")}
                          >
                            <Check size={15} />
                            Принять кандидат
                          </button>
                          <button
                            className="button secondary"
                            disabled={
                              readOnly || (!live && item.author === "user")
                            }
                            onClick={() => confirmReview("reject")}
                          >
                            Запросить исправления
                          </button>
                        </>
                      ) : (
                        ["open", "needs_changes", "challenged"].includes(
                          item.status,
                        ) && (
                          <button
                            className="button secondary"
                            disabled={readOnly}
                            onClick={() =>
                              dispatch({
                                type: "SUBMIT_REVIEW",
                                target: item.id,
                              })
                            }
                          >
                            <ClipboardCheck size={15} />
                            Направить на проверку
                          </button>
                        )
                      )}
                      <button
                        className="button secondary"
                        disabled={readOnly}
                        onClick={() => openModal("finding")}
                      >
                        <Plus size={15} />
                        Добавить замечание
                      </button>
                    </div>
                  </>
                )}
                {detailTab === "questions" && (
                  <>
                    <QuestionList
                      questions={view.questions.filter(
                        (q) => q.target === item.id,
                      )}
                      onSelect={select}
                      compact
                    />
                    <form
                      className="question-form"
                      onSubmit={async (e) => {
                        e.preventDefault();
                        if (
                          await dispatch({
                            type: "QUESTION",
                            target: item.id,
                            text: question,
                            kind: questionKind,
                          })
                        )
                          setQuestion("");
                      }}
                    >
                      <label htmlFor="question-kind">Тип сообщения</label>
                      <select
                        id="question-kind"
                        value={questionKind}
                        onChange={(e) => setQuestionKind(e.target.value)}
                        disabled={readOnly}
                      >
                        <option value="question">Уточняющий вопрос</option>
                        <option value="proposal">
                          Предложение исследования
                        </option>
                      </select>
                      <label htmlFor="question-text">
                        Сообщение к {item.id}@{item.revision}
                      </label>
                      <textarea
                        id="question-text"
                        placeholder="Какие условия еще не проверены?"
                        value={question}
                        maxLength={4000}
                        onChange={(e) => setQuestion(e.target.value)}
                        disabled={readOnly}
                        required
                      />
                      <button
                        className="button primary"
                        disabled={readOnly || !question.trim()}
                      >
                        <Send size={15} />
                        Отправить
                      </button>
                    </form>
                  </>
                )}
              </div>
              <div className="inspector-footer">
                <span>Снимок v{view.revision}</span>
                <button onClick={() => setSection("history")}>
                  История <ArrowUpRight size={13} />
                </button>
              </div>
            </aside>
          )}
        </div>
      </main>
      {toast && (
        <div role="status" className="toast">
          <CircleCheck size={17} />
          <span>{toast}</span>
          <IconButton
            icon={X}
            label="Закрыть уведомление"
            onClick={() => setToast("")}
          />
        </div>
      )}
      {modal && (
        <Dialog
          modal={modal}
          state={state}
          study={study}
          live={!!live}
          busy={readOnly}
          onClose={closeModal}
          onSubmit={async (action) => {
            if (action.type === "RESET" && !live) {
              setState(createInitialState());
              setHistoric(null);
              setSelected("H0");
              setStudy("forest");
              setSection("research");
              setTab("map");
              setDetailTab("overview");
              setToast("Демонстрационные данные восстановлены");
              closeModal();
              return;
            }
            if (await dispatch(action)) closeModal();
          }}
        />
      )}
    </div>
  );
}
function Empty({ icon: Icon, title, text }) {
  return (
    <div className="empty">
      <Icon size={26} />
      <h3>{title}</h3>
      <p>{text}</p>
    </div>
  );
}
function QuestionList({ questions, onSelect, compact }) {
  if (!questions.length)
    return (
      <Empty
        icon={MessageSquare}
        title="Вопросов пока нет"
        text={
          compact
            ? "Объект обсуждения выбран."
            : "В этом исследовании еще нет сообщений."
        }
      />
    );
  return (
    <div className="questions">
      {questions.map((q) => (
        <article className="question-entry" key={q.id}>
          <div>
            <button className="text-link" onClick={() => onSelect(q.target)}>
              {q.target}
            </button>
            <span>Снимок v{q.snapshot}</span>
            <span>{q.kind === "proposal" ? "Предложение" : "Вопрос"}</span>
          </div>
          <p className="question-text">{q.text}</p>
          <div className="answer">
            <strong>
              {q.answerAttempt
                ? "Ответ исполнителя"
                : q.answer
                  ? "Сведения из макета"
                  : "Ответ еще не получен"}
            </strong>
            <p>
              {q.answer ||
                (q.kind === "proposal"
                  ? "Предложение записано."
                  : "Задание ожидает запуска.")}
            </p>
          </div>
        </article>
      ))}
    </div>
  );
}
function Dialog({
  modal,
  state,
  study,
  onClose,
  onSubmit,
  live = false,
  busy = false,
}) {
  const ref = useRef(null);
  useEffect(() => {
    const previous = document.activeElement;
    const focusable = () => [
      ...ref.current.querySelectorAll(
        "button:not([disabled]),input,textarea,select",
      ),
    ];
    focusable()[0]?.focus();
    const trap = (e) => {
      if (e.key === "Tab") {
        const list = focusable(),
          first = list[0],
          last = list.at(-1);
        if (e.shiftKey && document.activeElement === first) {
          e.preventDefault();
          last.focus();
        } else if (!e.shiftKey && document.activeElement === last) {
          e.preventDefault();
          first.focus();
        }
      }
    };
    ref.current.addEventListener("keydown", trap);
    return () => {
      previous?.focus();
    };
  }, []);
  const names = {
    lemma: "Предложить лемму",
    split: "Разбить на случаи",
    apply: "Применить лемму",
    finding: "Замечание к доказательству",
    challenge: "Оспорить основание",
    accept: "Приемка кандидата",
    reject: "Запросить исправления",
    reset: "Сбросить данные макета",
    study: "Создать исследование",
    closeFinding: "Результат перепроверки",
  };
  const target = state.entities.find((x) => x.id === modal.target);
  return (
    <div
      className="modal-backdrop"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <section
        className="modal"
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-labelledby="dialog-title"
      >
        <header>
          <h2 id="dialog-title">{names[modal.kind]}</h2>
          <IconButton icon={X} label="Закрыть диалог" onClick={onClose} />
        </header>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            const v = Object.fromEntries(new FormData(e.currentTarget));
            const actions = {
              lemma: { type: "LEMMA", ...v, study },
              study: { type: "CREATE_STUDY", ...v },
              closeFinding: {
                type: "CLOSE_FINDING",
                finding: modal.target,
                text: v.text,
              },
              split: {
                type: "SPLIT",
                target: modal.target,
                parts: [v.first, v.second],
              },
              apply: { type: "APPLY", lemma: modal.target, target: v.target },
              finding: { type: "FINDING", target: modal.target, ...v },
              challenge: {
                type: "CHALLENGE",
                target: modal.target,
                text: v.text,
              },
              accept: {
                type: "REVIEW",
                target: modal.target,
                decision: "accept",
                text: v.text,
              },
              reject: {
                type: "REVIEW",
                target: modal.target,
                decision: "reject",
                text: v.text,
              },
              reset: { type: "RESET" },
            };
            onSubmit(actions[modal.kind]);
          }}
        >
          {["lemma", "study"].includes(modal.kind) && (
            <>
              <label>
                Название
                <input name="title" required maxLength={100} />
              </label>
              <label>
                Точное утверждение
                <textarea name="statement" required maxLength={2000} />
              </label>
              <label>
                Область и предпосылки
                <textarea name="assumptions" required maxLength={1500} />
              </label>
              <p className="form-note">
                Результат будет сохранен как непроверенный кандидат.
              </p>
            </>
          )}
          {modal.kind === "split" && (
            <>
              <p className="form-note">{target?.statement}</p>
              <label>
                Первый случай
                <input name="first" required maxLength={120} />
              </label>
              <label>
                Второй случай
                <input name="second" required maxLength={120} />
              </label>
              <p className="form-note">
                Для покрытия исходной области создается отдельное обязательство.
              </p>
            </>
          )}
          {modal.kind === "apply" && (
            <>
              <p>
                <strong>{target?.title}</strong>
              </p>
              <p className="form-note">{target?.assumptions}</p>
              <label>
                Целевая гипотеза
                <select
                  name="target"
                  defaultValue={
                    state.studies.find((x) => x.id !== target?.study)?.goal
                  }
                >
                  {state.entities
                    .filter(
                      (x) =>
                        ["goal", "claim"].includes(x.kind) &&
                        !["accepted", "refuted"].includes(x.status),
                    )
                    .map((x) => (
                      <option value={x.id} key={x.id}>
                        {x.id}: {x.title}
                      </option>
                    ))}
                </select>
              </label>
              <p className="form-note">
                Применимость и перенос результата потребуют отдельной проверки.
              </p>
            </>
          )}
          {["finding", "challenge"].includes(modal.kind) && (
            <>
              <p className="form-note">
                {target?.id}@{target?.revision}: {target?.title}
              </p>
              {modal.kind === "finding" && (
                <label>
                  Категория
                  <select name="severity">
                    <option value="question">Вопрос к переходу</option>
                    <option value="major">Существенное замечание</option>
                    <option value="editorial">Изложение</option>
                  </select>
                </label>
              )}
              <label>
                Основание замечания
                <textarea name="text" required maxLength={3000} />
              </label>
              {modal.kind === "challenge" && (
                <p className="form-note">
                  Зависимые результаты потребуют пересмотра. Оспаривание не
                  объявляет теорему ложной.
                </p>
              )}
            </>
          )}
          {live &&
            ["accept", "reject", "closeFinding"].includes(modal.kind) && (
              <label>
                Обоснование решения
                <textarea name="text" required maxLength={4000} />
              </label>
            )}
          {modal.kind === "accept" && !live && (
            <p>
              Записать приемку {target?.id} в демонстрационном журнале? Реальная
              проверка доказательства не выполняется.
            </p>
          )}
          {modal.kind === "reject" && (
            <p>
              Вернуть {target?.id} на исправление? Это не означает опровержения
              утверждения.
            </p>
          )}
          {modal.kind === "reset" && (
            <p>
              Локальные вопросы, предложения и журнал действий будут заменены
              исходным демонстрационным снимком.
            </p>
          )}
          <footer>
            <button
              type="button"
              className="button secondary"
              onClick={onClose}
            >
              Отмена
            </button>
            <button className="button primary" type="submit" disabled={busy}>
              {modal.kind === "reset" ? "Сбросить" : "Подтвердить"}
            </button>
          </footer>
        </form>
      </section>
    </div>
  );
}
const demonstration =
  new URLSearchParams(window.location.search).get("demo") === "1";
createRoot(document.getElementById("root")).render(
  demonstration ? <App /> : <Connected App={App} />,
);
