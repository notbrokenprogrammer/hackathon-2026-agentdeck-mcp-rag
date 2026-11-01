"use client"

import { useState, type FormEvent } from "react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import { ArrowRight, LockKeyhole } from "lucide-react"
import { requestJson } from "@/lib/utils/api"

export function AuthForm({ mode }: { mode: "login" | "register" }) {
  const [notice, setNotice] = useState("")
  const [isSubmitting, setIsSubmitting] = useState(false)
  const router = useRouter()
  const isLogin = mode === "login"

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setIsSubmitting(true)
    setNotice("")

    const formData = new FormData(event.currentTarget)
    const payload = {
      name: formData.get("name")?.toString().trim() ?? "",
      email: formData.get("email")?.toString().trim() ?? "",
      password: formData.get("password")?.toString() ?? "",
    }

    if (!payload.email || !payload.password || (!isLogin && !payload.name)) {
      setNotice("Проверь, что все поля заполнены корректно.")
      setIsSubmitting(false)
      return
    }

    try {
      const data = await requestJson(`/api/auth/${isLogin ? "login" : "register"}`, {
        method: "POST",
        body: JSON.stringify(payload),
      })

      setNotice(isLogin ? "Вход выполнен. Перенаправляю..." : "Аккаунт создан. Перенаправляю...")
      router.push("/")
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "Не удалось подключиться к серверу")
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <div className="grid w-full max-w-5xl overflow-hidden rounded-2xl border border-border/70 bg-card/50 shadow-2xl shadow-black/10 backdrop-blur-xl lg:grid-cols-[0.9fr_1.1fr]">
      <div className="relative hidden overflow-hidden border-r border-border/60 bg-primary/5 p-10 lg:block">
        <p className="font-mono text-xs uppercase tracking-[0.3em] text-primary">
          {isLogin ? "AUTHORIZATION" : "CREATE ACCOUNT"}
        </p>
        <div className="mt-16 space-y-5 font-mono text-sm leading-7">
          <p><span className="text-primary">&gt;</span> access: student</p>
          <p><span className="text-primary">&gt;</span> subject: mathematics</p>
          <p><span className="text-primary">&gt;</span> progress: saved</p>
          <p><span className="text-primary">&gt;</span> session: secure</p>
          <p><span className="text-primary">&gt;</span> status: waiting for {isLogin ? "login" : "registration"}</p>
        </div>
        <div className="absolute -bottom-24 -left-24 h-72 w-72 rounded-full bg-primary/15 blur-3xl" />
      </div>

      <div className="p-6 sm:p-10 lg:p-12">
        <div className="mb-9">
          <p className="mb-3 font-mono text-xs uppercase tracking-[0.3em] text-primary lg:hidden">
            {isLogin ? "AUTHORIZATION" : "CREATE ACCOUNT"}
          </p>
          <h1 className="text-3xl font-bold tracking-tight sm:text-4xl">
            {isLogin ? "Вход в fizma" : "Регистрация в fizma"}
          </h1>
          <p className="mt-4 text-sm leading-6 text-muted-foreground">
            {isLogin
              ? "Продолжи подготовку и вернись к сохранённым результатам."
              : "Создай аккаунт, чтобы решать задания, сохранять результаты и видеть свой прогресс."}
          </p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-5">
          {!isLogin && (
            <label className="block">
              <span className="mb-2 block font-mono text-xs uppercase tracking-wider text-muted-foreground">Имя</span>
              <input
                type="text"
                name="name"
                autoComplete="name"
                required
                className="w-full rounded-lg border border-input bg-background/60 px-4 py-3.5 text-sm text-foreground transition-colors placeholder:text-muted-foreground/60 focus:border-primary"
                placeholder="Как к тебе обращаться"
              />
            </label>
          )}

          <label className="block">
            <span className="mb-2 block font-mono text-xs uppercase tracking-wider text-muted-foreground">Электронная почта</span>
            <input
              type="email"
              name="email"
              autoComplete="email"
              required
              className="w-full rounded-lg border border-input bg-background/60 px-4 py-3.5 text-sm text-foreground transition-colors placeholder:text-muted-foreground/60 focus:border-primary"
              placeholder="student@example.com"
            />
          </label>

          <label className="block">
            <span className="mb-2 block font-mono text-xs uppercase tracking-wider text-muted-foreground">Пароль</span>
            <input
              type="password"
              name="password"
              autoComplete={isLogin ? "current-password" : "new-password"}
              required
              minLength={8}
              className="w-full rounded-lg border border-input bg-background/60 px-4 py-3.5 text-sm text-foreground transition-colors placeholder:text-muted-foreground/60 focus:border-primary"
              placeholder="Не менее 8 символов"
            />
          </label>

          <button
            type="submit"
            disabled={isSubmitting}
            className="group relative flex w-full items-center justify-center gap-3 overflow-hidden rounded-lg border border-primary bg-primary/10 px-6 py-4 font-mono text-sm text-primary transition-all duration-500 hover:text-primary-foreground disabled:pointer-events-none disabled:opacity-70"
          >
            <span className="absolute inset-0 -translate-x-full bg-primary transition-transform duration-500 group-hover:translate-x-0" />
            <span className="relative z-10">{isSubmitting ? (isLogin ? "ВХОД..." : "СОЗДАНИЕ...") : (isLogin ? "ВОЙТИ" : "СОЗДАТЬ АККАУНТ")}</span>
            <ArrowRight className="relative z-10 h-4 w-4 transition-transform duration-300 group-hover:translate-x-1" />
          </button>
        </form>

        {notice && (
          <div className="mt-5 flex items-start gap-3 rounded-lg border border-primary/35 bg-primary/10 p-4 text-sm leading-6 text-muted-foreground">
            <LockKeyhole className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
            {notice}
          </div>
        )}

        <p className="mt-7 text-center text-sm text-muted-foreground">
          {isLogin ? "Нет аккаунта?" : "Уже есть аккаунт?"}{" "}
          <Link href={isLogin ? "/register" : "/login"} className="font-medium text-primary hover:underline">
            {isLogin ? "Зарегистрироваться" : "Войти"}
          </Link>
        </p>
      </div>
    </div>
  )
}
