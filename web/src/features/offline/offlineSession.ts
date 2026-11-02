import { releaseOfflineOwner, syncQueuedOfflineProgress } from './offlineReader';

// Explicit logout has one last upload opportunity while the session is still valid.
// Expiry and network failure do not call this: only logout relinquishes device data.
export async function endOfflineSession(closeSession: () => Promise<unknown>): Promise<void> {
  try {
    await syncQueuedOfflineProgress();
  } catch {
    // Failed uploads must not prevent leaving a shared device.
  }
  try {
    await closeSession();
  } catch {
    // Server unavailability does not retain local ownership.
  }
  try {
    releaseOfflineOwner();
  } catch {
    // Unavailable localStorage must not prevent logout.
  }
}
