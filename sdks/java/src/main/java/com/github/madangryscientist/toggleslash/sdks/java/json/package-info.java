/**
 * A JSON parser and writer, and the UTF-8 codec the ToggleSlash SDK needs.
 *
 * <p>Both are hand-written because the JDK has neither a JSON parser nor a UTF-8 codec that follows
 * the rules the evaluation spec and the SDK contract name (binary64 numbers, lone surrogates kept,
 * one U+FFFD per maximal invalid subpart).
 */
package com.github.madangryscientist.toggleslash.sdks.java.json;
