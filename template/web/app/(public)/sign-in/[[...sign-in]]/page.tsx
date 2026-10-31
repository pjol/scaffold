import type { Metadata } from "next";
import { Suspense } from "react";
import { SignInScreen } from "@/screens/public/SignInScreen";

export const metadata: Metadata = { title: "Sign in" };

export default function SignInPage() {
  return (
    <Suspense>
      <SignInScreen />
    </Suspense>
  );
}
