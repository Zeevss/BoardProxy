import { Icon } from '../components/Icon'

export function GettingStarted({ onCreateNode }: { onCreateNode: () => void }) {
  return <section className="getting-started">
    <div className="getting-started-copy">
      <h1>Подготовим первый узел</h1>
      <p>Панель готова. Создайте node-каталог и первую доску, получите одноразовый enrollment-секрет, затем запустите node-agent.</p>
      <button className="button primary" type="button" onClick={onCreateNode}><Icon name="plus" size={15}/> Создать первую ноду</button>
    </div>
    <ol className="setup-steps">
      <li className="done"><span>1</span><div><strong>Администратор</strong><small>Учётная запись создана</small></div></li>
      <li><span>2</span><div><strong>Node и доска</strong><small>Desired-state каталог</small></div></li>
      <li><span>3</span><div><strong>Enrollment</strong><small>Одноразовый секрет для node-agent</small></div></li>
      <li><span>4</span><div><strong>Пользователи</strong><small>Keylink или subscription</small></div></li>
    </ol>
  </section>
}
