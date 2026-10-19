import { createRoot } from "react-dom/client";
import { useIntl } from "react-intl";
import { LoginForm, LoginLocale } from "@/components/auth/login-form";
import { ThemeProvider } from "@/components/theme-provider";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { loginReturnURL } from "@/core/login-return-url";
import { installPagePinchZoomGuard } from "@/lib/prevent-page-pinch-zoom";
import "@/styles/globals.css";

function LoginPage({ returnURL }: { returnURL: string }) {
  const intl = useIntl();
  return (
    <main className="flex min-h-dvh items-center justify-center p-6">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>
            {intl.formatMessage({ id: "login.login", defaultMessage: "Login" })}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <LoginForm onSuccess={() => window.location.replace(returnURL)} />
        </CardContent>
      </Card>
    </main>
  );
}

installPagePinchZoomGuard();
const root = document.getElementById("root");
if (!root) throw new Error("Missing application root");
const returnURL = loginReturnURL(
  new URLSearchParams(window.location.search).get("returnURL"),
);
createRoot(root).render(
  <LoginLocale>
    <ThemeProvider>
      <LoginPage returnURL={returnURL} />
    </ThemeProvider>
  </LoginLocale>,
);
