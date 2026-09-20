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
  if (!response.ok)
    throw new APIError(
      value?.error ||
        (response.status === 401
          ? "Нужен вход."
          : "Запрос не выполнен. Обновите состояние."),
      response.status,
    );
  if (text && value === null) throw new APIError("Сервер API недоступен.", 502);
  return value;
}
export const commandID = () => crypto.randomUUID();
