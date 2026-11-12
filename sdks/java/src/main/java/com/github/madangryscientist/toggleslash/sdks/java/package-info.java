/**
 * The ToggleSlash server SDK for Java.
 *
 * <p>{@link com.github.madangryscientist.toggleslash.sdks.java.ToggleSlashClient} holds one
 * environment's ruleset, keeps it fresh from the server, evaluates flags locally and posts usage
 * counts. It never throws at a caller from an evaluation: a problem is a fallback value and a
 * {@link com.github.madangryscientist.toggleslash.sdks.java.Reason}.
 */
package com.github.madangryscientist.toggleslash.sdks.java;
