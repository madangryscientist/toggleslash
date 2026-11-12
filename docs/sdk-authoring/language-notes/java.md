# Java

What the Java runs learned. Each fact cites its run: prompt `7622421`, FS-97, 2026-11-10, unless it is marked (FS-151), the run at prompt `01bdb48` on 2026-11-12, or (FS-153), the run at prompt `19b29aa` on 2026-11-12.

- **Toolchain.** Pin the newest LTS (Temurin 25 in that run). A feature release such as the current
  non-LTS one leaves support after six months. Minimum Java 17 is separate, set with `--release 17`.
- **Maven group.** Use the reverse-domain form of the repository path: group `com.github.madangryscientist.toggleslash`,
  artifact `toggleslash-sdk-java`, root package `<groupId>.sdks.java` (from `sdks/java`).
- **`HttpClient` and the body.** `HttpRequest.timeout` ends at the response headers, not the body. Do not use
  `BodyHandlers.fromSubscriber` for the stream: it answers the response future only when the body ends, so
  the stream open timed out and 23 stream scenarios failed with `lastError: timeout`. Make the body a
  `BodySubscriber` that hands itself over at once.
- **No closable `HttpClient` before 21.** `AutoCloseable` came in 21, so `close` cannot close it. Use two
  clients: abort the one for the ruleset and stream on `close`, and leave the one for usage so the final
  flush can still go out.
- **Checkstyle under mise is a bare JAR**, with no launcher. Resolve its path inside mise, as every tool is run (`mise exec -- sh -c 'command -v checkstyle'`): outside `mise exec`, `command -v checkstyle` finds the mise shim, a symlink that `java -jar` rejects as a corrupt jarfile. Then run `java -jar "$jar" -c checkstyle.xml src/main/java` (FS-153).
- **First `mvn` run is online.** It caches the plugins. A fresh checkout needs the network once; later runs
  work offline.
- **Test service layout.** `build-helper-maven-plugin` adds `test-service/src` as a test source root, so
  `mvn test` compiles it with the tests. `bin/test-service` runs `java -cp target/classes:target/test-classes`
  and builds only when the classes are missing.
- **The formatter on the pinned JDK.** google-java-format 1.37.0 (the newest when the run wrote its pom) fails on JDK 25 under Spotless (it reaches into javac internals); set `<googleJavaFormat><version>` to 2.0.0 or later explicitly (FS-151).
- **Do not rely on the JDK's UTF-8 codec for the spec.** A `CharsetDecoder` with `REPLACE` and `String.getBytes(UTF_8)` differ from the WHATWG rules the contract names (one U+FFFD per maximal invalid subpart; a lone surrogate must not become `?`). Write the decoder and encoder and test them against the examples in the Encoding standard (FS-151).
- **Checkstyle 14 traps.** It has no `JavadocStyle` check; use `SummaryJavadoc` and `JavadocParagraph`. `LineLength` belongs under `Checker`, not `TreeWalker`. A `{@code}` with unbalanced braces breaks the Javadoc parser (FS-153).
