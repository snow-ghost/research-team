export function verificationCurrent(verification, entity, state) {
  return (
    verification.target_revision === entity.revision &&
    Object.entries(verification.library_pins || {}).every(([id, digest]) =>
      state.library?.some(
        (l) =>
          l.id === id &&
          l.status === "ready" &&
          l.report?.artifact_sha256 === digest,
      ),
    )
  );
}
