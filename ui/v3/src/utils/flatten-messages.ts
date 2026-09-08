type NestedMessage = { [key: string]: NestedMessage | string };
const flattenMessages = (nestedMessages: NestedMessage | null, prefix = "") => {
  if (nestedMessages === null) {
    return {};
  }
  return Object.entries(nestedMessages).reduce<Record<string, string>>(
    (messages, [key, value]) => {
      const prefixedKey = prefix ? `${prefix}.${key}` : key;

      if (typeof value === "string") {
        Object.assign(messages, { [prefixedKey]: value });
      } else {
        Object.assign(messages, flattenMessages(value, prefixedKey));
      }

      return messages;
    },
    {},
  );
};

export default flattenMessages;
