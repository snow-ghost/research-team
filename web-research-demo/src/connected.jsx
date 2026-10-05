import React, { useCallback, useEffect, useRef, useState } from "react";
import { Network, KeyRound, LogIn, RefreshCw, CircleAlert } from "lucide-react";
import { api, commandID } from "./api.js";

export function Connected({ App }) {
  const [session, setSession] = useState(undefined);
  const [data, setData] = useState(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const lock = useRef(false);
  const accept = useCallback(
    (next) =>
      setData((old) =>
        !old || next.state.revision >= old.state.revision ? next : old,
      ),
    [],
  );
  const refresh = useCallback(async () => {
    try {
      const next = await api("/bootstrap");
      accept(next);
      setError("");
      return next;
    } catch (e) {
      if (e.status === 401) {
        setSession(null);
        setData(null);
      }
      setError(e.message);
      throw e;
    }
  }, [accept]);
  useEffect(() => {
    let active = true;
    api("/session")
      .then((value) => {
        if (active) setSession(value);
      })
      .catch((e) => {
        if (active) {
          setSession(null);
          if (e.status !== 401) setError(e.message);
        }
      });
    return () => {
      active = false;
    };
  }, []);
  useEffect(() => {
    if (!session) return;
    refresh().catch(() => {});
    const timer = setInterval(() => refresh().catch(() => {}), 2000);
    return () => clearInterval(timer);
  }, [session, refresh]);
  async function post(path, body, versioned = false) {
    if (lock.current) throw new Error("Предыдущее действие еще выполняется.");
    lock.current = true;
    setBusy(true);
    try {
      const payload = versioned
        ? {
            ...body,
            expected_revision: data.state.revision,
            request_id: commandID(),
          }
        : body;
      const result = await api(path, {
        method: "POST",
        body: payload,
        csrf: session.csrf,
      });
      if (result?.state) accept(result);
      return result;
    } catch (e) {
      if (e.status === 401) {
        setSession(null);
        setData(null);
      }
      await refresh().catch(() => {});
      throw e;
    } finally {
      lock.current = false;
      setBusy(false);
    }
  }
  if (!session)
    return (
      <main className="access-screen">
        <div className="access-brand">
          <Network size={28} />
          <h1>Research Team</h1>
        </div>
        <form
          className="access-form"
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            setError("");
            const form = e.currentTarget;
            try {
              const value = await api("/session", {
                method: "POST",
                body: { token: new FormData(form).get("token").trim() },
              });
              form.reset();
              setSession(value);
            } catch (err) {
              setError(err.message);
            } finally {
              setBusy(false);
            }
          }}
        >
          <label>
            <KeyRound size={16} /> Ключ доступа
            <input
              name="token"
              type="password"
              autoComplete="current-password"
              required
              disabled={busy || session === undefined}
            />
          </label>
          <button
            className="button primary"
            disabled={busy || session === undefined}
          >
            <LogIn size={16} />
            Войти
          </button>
          {error && (
            <p role="alert" className="api-error">
              <CircleAlert size={16} />
              {error}
            </p>
          )}
        </form>
      </main>
    );
  if (!data)
    return (
      <main className="access-screen">
        <h1>Research Team</h1>
        <p role="status">{error || "Загрузка рабочей области..."}</p>
        <button
          className="button secondary"
          onClick={() => refresh().catch(() => {})}
        >
          <RefreshCw size={16} />
          Обновить
        </button>
      </main>
    );
  return (
    <App
      live={{
        data,
        busy,
        error,
        refresh,
        action: (action) => post("/actions", action, true),
        command: (path, body) => post(path, body, true),
        start: (body) => post("/attempts", body, true),
        startCycle: (body) => post("/cycles", body, true),
        controlCycle: (id, body) =>
          post("/cycles/" + encodeURIComponent(id) + "/commands", body, true),
        cancel: (id) =>
          post("/attempts/" + encodeURIComponent(id) + "/cancel", {}),
        prepareCoddy: (body) => post("/coddy", body, true),
        coddyCommand: (id, body) =>
          post("/coddy/" + encodeURIComponent(id) + "/commands", body),
        get: (path) => api(path),
        logout: async () => {
          await api("/session", {
            method: "DELETE",
            body: {},
            csrf: session.csrf,
          });
          setSession(null);
          setData(null);
        },
      }}
    />
  );
}
