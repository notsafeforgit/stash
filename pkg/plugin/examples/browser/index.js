export default function register(host) {
  function ExamplePage() {
    return host.react.createElement("p", null, "The browser plugin is active.");
  }

  host.routes.add({ path: "/example", component: ExamplePage });
  host.nav.add({ label: "Browser example", to: "/example" });
}
