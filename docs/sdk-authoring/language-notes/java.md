# Java

What the Java runs learned. Each fact cites its run: prompt `7622421`, FS-97, 2026-11-10, unless it is marked (FS-151), the run at prompt `01bdb48` on 2026-11-12, (FS-153), the run at prompt `19b29aa` on 2026-11-12, (FS-157), the run at prompt `f83c46d` on 2026-11-12, (FS-159), the run at prompt `29ae975` on 2026-11-12, or (FS-161), the run at prompt `546ffdb` on 2026-11-12.

- **Toolchain.** Pin the newest LTS (Temurin 25 in that run). A feature release such as the current
  non-LTS one leaves support after six months. Minimum Java 17 is separate, set with `--release 17`.
- **Maven group.** Use the reverse-domain form of the repository path: group `com.github.madangryscientist.toggleslash`,
  artifact `toggleslash-sdk-java`, root package `<groupId>.sdks.java` (from `sdks/java`).
- **`HttpClient` and the body.** `HttpRequest.timeout` ends at the response headers, not the body. Do not use
  `BodyHandlers.fromSubscriber` for the stream: it answers the response future only when the body ends, so
  the stream open timed out and 23 stream scenarios failed with `lastError: timeout`. Make the body a
  `BodySubscriber` that hands itself over at once.
- **No closable `HttpClient` before 21.** `AutoCloseable` came in 21, so `close` cannot close it. What `close` must do is contract section 11: abandon the ruleset and stream requests in flight, then send the final usage flush. Keep each in-flight ruleset and stream request as a `CompletableFuture` and cancel those on `close`; one client with that per-call abort is enough, and two clients (abort the one for the ruleset and stream, keep the one for usage) also work (FS-157).
- **Checkstyle under mise is a bare JAR**, with no launcher. Resolve its path inside mise, as every tool is run (`mise exec -- sh -c 'command -v checkstyle'`): outside `mise exec`, `command -v checkstyle` finds the mise shim, a symlink that `java -jar` rejects as a corrupt jarfile. Then run `java -jar "$jar" -c checkstyle.xml src/main/java` (FS-153).
- **First `mvn` run is online.** It caches the plugins. A fresh checkout needs the network once; later runs
  work offline.
- **Test service layout.** `build-helper-maven-plugin` adds `test-service/src` as a test source root, so
  `mvn test` compiles it with the tests. `bin/test-service` runs `java -cp target/classes:target/test-classes`
  and builds only when the classes are missing.
- **The formatter on the pinned JDK.** google-java-format 1.37.0 (the newest when the run wrote its pom) fails on JDK 25 under Spotless (it reaches into javac internals); set `<googleJavaFormat><version>` to 2.0.0 or later explicitly (FS-151).
- **Do not rely on the JDK's UTF-8 codec for the spec.** A `CharsetDecoder` with `REPLACE` and `String.getBytes(UTF_8)` differ from the WHATWG rules the contract names (one U+FFFD per maximal invalid subpart; a lone surrogate must not become `?`). Write the decoder and encoder and test them against the examples in the Encoding standard (FS-151).
- **Lint and an explicit `close()`.** `-Xlint:all -Werror` rejects an explicit `close()` on an `AutoCloseable`
  that is also a try-with-resources variable ("explicit call to close()"); a test that closes a client to test
  `close` declares it outside the `try` or suppresses that one lint on the test method (run 27, FS-170).
- **Checkstyle 14 traps.** It has no `JavadocStyle` check; use `SummaryJavadoc` and `JavadocParagraph`. `LineLength` belongs under `Checker`, not `TreeWalker`. `JavadocVariable` and `JavadocMethod` take `accessModifiers` (a lint that skips package-private methods sets the latter to `public, protected`, run 27, FS-170), while `MissingJavadocMethod` and `MissingJavadocType` take `scope`; `JavadocPackage` needs a `package-info.java` in every package; `JavadocType` needs `allowMissingParamTags` set to true for a record or a type with nothing to document (FS-157). A `{@code}` with unbalanced braces breaks the Javadoc parser (FS-153).
- **google-java-format exits 0 on a dry run.** `--dry-run <files>` prints the files it would change and exits 0, so `bin/lint` passes `--set-exit-if-changed` or fails on any output. The mise build is a native binary and runs on JDK 25. Checkstyle's `SummaryJavadoc` wants the first sentence to end in a full stop, so a question (`/** Does ...? */`) fails (FS-159).
- **CI toolchain.** `jdx/mise-action` with `install_args: java maven checkstyle google-java-format` installs the whole toolchain from `mise.toml`, the two lint tools included, which `actions/setup-java` does not provide (FS-159).
- **Generic accessor and numbers.** The JSON parser yields doubles, so a generic accessor whose fallback is an `Integer` or `Long` converts a whole served number to that class and answers `TYPE_MISMATCH` for a fraction or an out-of-range value (contract section 4, FS-161).
