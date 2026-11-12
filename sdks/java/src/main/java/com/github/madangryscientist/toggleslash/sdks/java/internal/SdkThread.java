package com.github.madangryscientist.toggleslash.sdks.java.internal;

/** A daemon thread the SDK started; code can tell it from a caller's thread. */
public final class SdkThread extends Thread {
  /**
   * Creates the thread.
   *
   * @param name the thread name
   * @param body what it runs
   */
  public SdkThread(String name, Runnable body) {
    super(body, name);
    setDaemon(true);
  }
}
