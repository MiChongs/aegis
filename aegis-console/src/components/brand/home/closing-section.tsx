"use client";

import Link from "next/link";
import { ArrowRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { closing } from "@/components/brand/home/home-content";
import { Reveal, Section } from "@/components/brand/home/section";
import { Grain, Pattern } from "@/components/brand/home/visuals";
import { useAuthStore } from "@/lib/auth-store";

export function ClosingSection() {
  const hydrated = useAuthStore((state) => state.hydrated);
  const accessToken = useAuthStore((state) => state.accessToken);
  const authenticated = hydrated && Boolean(accessToken);

  return (
    <Section bordered className="relative overflow-hidden bg-muted/30">
      <Grain />

      <Reveal>
        {/* home-beam-frame 让一段强调色沿边框跑一圈：整页到这里只剩一个动作，
            这条流光的作用是把视线收回到那个按钮上。 */}
        <div className="home-beam-frame relative overflow-hidden rounded-xl">
          <div className="relative overflow-hidden rounded-xl border bg-card px-6 py-14 text-center md:px-12 md:py-20">
            <Pattern
              size={48}
              mask="radial-gradient(ellipse 55% 60% at 50% 50%, #000, transparent 75%)"
            />
            <div
              aria-hidden
              className="pointer-events-none absolute inset-x-0 top-0 h-40"
              style={{
                background:
                  "radial-gradient(ellipse 50% 100% at 50% 0%, color-mix(in srgb, var(--home-accent) 12%, transparent), transparent 70%)",
              }}
            />

            <div className="relative flex flex-col items-center gap-6">
              <h2 className="text-2xl font-semibold tracking-tight text-balance md:text-4xl">
                {closing.title}
              </h2>
              <p className="max-w-lg text-sm leading-relaxed text-pretty text-muted-foreground md:text-base">
                {closing.description}
              </p>
              <div className="mt-2 flex flex-wrap justify-center gap-3 max-sm:w-full max-sm:flex-col">
                <Button
                  asChild
                  size="lg"
                  className="shadow-[0_8px_30px_-10px_var(--home-beam)]"
                >
                  <Link href={authenticated ? "/overview" : "/login"}>
                    {authenticated ? closing.primary.authed : closing.primary.guest}
                    <ArrowRight />
                  </Link>
                </Button>
                <Button asChild size="lg" variant="outline">
                  <Link href={closing.secondary.href}>{closing.secondary.label}</Link>
                </Button>
              </div>
            </div>
          </div>
        </div>
      </Reveal>
    </Section>
  );
}
