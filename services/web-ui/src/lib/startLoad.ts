/**
 * Starts an async load from an effect without waiting for it.
 *
 * react-hooks 7 flags an effect that calls a component function which sets
 * state, on the assumption that the call is synchronous. A data load is not:
 * its state changes happen when the response arrives, which is the pattern
 * the rule's own documentation accepts ("calling setState in a callback when
 * external state changes"). This makes that explicit at the call site, and
 * reports a rejected load instead of leaving an unhandled rejection.
 */
export function startLoad(load: () => Promise<unknown>): void {
    void (async () => {
        try {
            await load();
        } catch (e) {
            console.error(e);
        }
    })();
}
