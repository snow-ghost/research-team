import React, { useEffect, useState } from "react";

export function ResearchMethodSelect({ live }) {
  const [methods, setMethods] = useState([]);
  const [error, setError] = useState("");
  useEffect(() => {
    let cancelled = false;
    live
      .get("/research-methods")
      .then((value) => {
        if (!cancelled) setMethods(value);
      })
      .catch((err) => {
        if (!cancelled) setError(err.message);
      });
    return () => {
      cancelled = true;
    };
  }, [live.get]);
  return (
    <label>
      Метод исследования
      <select name="method" defaultValue="">
        <option value="">Не выбран</option>
        {methods.map((m) => (
          <option key={m.id} value={m.id}>
            {m.label}
          </option>
        ))}
      </select>
      {error && (
        <span role="alert" className="api-error">
          {error}
        </span>
      )}
    </label>
  );
}
