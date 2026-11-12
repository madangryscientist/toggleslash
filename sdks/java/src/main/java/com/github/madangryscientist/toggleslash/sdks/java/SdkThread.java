package com.github.madangryscientist.toggleslash.sdks.java;

/**
 * A daemon thread of the SDK's own, so {@code ready} and {@code close} can tell they run on one.
 */
final class SdkThread extends Thread {
  SdkThread(Runnable task, String name) {
    super(task, name);
    setDaemon(true);
  }
}
