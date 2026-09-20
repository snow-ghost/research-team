export class APIError extends Error {
  constructor(message, status) {
    super(message);
    this.status = status;
  }
}
export async function api(path, { method = "GET", body, csrf, signal } = {}) {
  const response = await fetch("/api" + path, {
    method,
    credentials: "same-origin",
    signal,
    headers:
      body === undefined
        ? {}
        : {
            "Content-Type": "application/json",
            ...(csrf ? { "X-Research-CSRF": csrf } : {}),
          },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  const text = await response.text();
  let value;
  try {
    value = text ? JSON.parse(text) : null;
  } catch {
    value = null;
  }
  const message =
    typeof value?.error === "string"
      ? value.error
      : response.headers.get("content-type")?.split(";")[0].trim() ===
          "text/plain"
        ? text.trim().slice(0, 500)
        : "";
  if (!response.ok)
    throw new APIError(
      message ||
        (response.status === 401
          ? "Нужен вход."
          : "Запрос не выполнен. Обновите состояние."),
      response.status,
    );
  if (text && value === null) throw new APIError("Сервер API недоступен.", 502);
  return value;
}
export const commandID = () => crypto.randomUUID();
