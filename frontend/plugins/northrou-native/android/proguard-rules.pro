-keep @app.tauri.annotation.TauriPlugin public class * {
  @app.tauri.annotation.Command public <methods>;
  @app.tauri.annotation.PermissionCallback <methods>;
  @app.tauri.annotation.ActivityCallback <methods>;
  @app.tauri.annotation.Permission <methods>;
  public <init>(...);
}

-keep @app.tauri.annotation.InvokeArg public class * {
  *;
}
