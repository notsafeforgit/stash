class StashEvent extends EventTarget {
  dispatch(event: string, id?: string, data?: object) {
    const name = `stash:${event}${id ? `:${id}` : ""}`;

    this.dispatchEvent(
      new CustomEvent(name, {
        detail: {
          event: name,
          ...(id ? { id } : {}),
          ...(data ? { data } : {}),
        },
      }),
    );
  }
}

const Event = new StashEvent();

export default Event;
